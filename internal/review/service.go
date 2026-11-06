package review

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Service orchestrates review sessions over member repos: it resolves
// targets (fetching PR heads via go-git), creates the dedicated review
// worktree through the store's seam, and produces diffs with the
// user's git. Persistence lives in Store; this type holds no
// state beyond its deps.
type Service struct {
	ws     *workspace.Workspace
	store  *Store
	runner *gitcore.Runner // nil = diff production unavailable (visible)
	gh     GH              // nil = PR inputs unavailable (visible)

	// diffFn is injectable for tests; production wraps runner.Run.
	diffFn func(ctx context.Context, dir string, args ...string) (string, error)
	// prBody is the PR body template (conventions.pr.body, F-042); "" keeps
	// the built-in line.
	prBody   string
	prBodyFn func() string // live source; wins over prBody when set
	// prTitleFn is the live PR title template (conventions.pr.title);
	// nil or "" = the author's title as given.
	prTitleFn func() string
	// prVars gives a branch's symbols ({slug}, {ticket}, {assignee},
	// {user}, {date} — F-075, F-182); nil = none.
	prVars func(branch string) map[string]string
	// generate writes AI slots for PRs a human opens (F-182); nil = such
	// a PR is refused by name.
	generate conventions.Generator
	// reviewBranch names a review's branch by the conventions (nil =
	// review/<id>, the default); the worktree's checkout wins over it.
	reviewBranch func(id string) (string, error)
}

// NewService wires the orchestration layer. runner and gh may be nil;
// affected flows degrade with visible errors.
func NewService(ws *workspace.Workspace, store *Store, runner *gitcore.Runner, gh GH) *Service {
	s := &Service{ws: ws, store: store, runner: runner, gh: gh}
	if runner != nil {
		s.diffFn = func(ctx context.Context, dir string, args ...string) (string, error) {
			stdout, _, err := runner.Run(ctx, dir, args...)
			return stdout, err
		}
	}
	return s
}

// Store exposes the persistence layer for surfaces.
func (s *Service) Store() *Store { return s.store }

// SetDiffForTest swaps the diff execution seam (tests only; mirrors the
// cardPathForTest convention).
func (s *Service) SetDiffForTest(fn func(ctx context.Context, dir string, args ...string) (string, error)) {
	s.diffFn = fn
}

// SetPRBodyFunc installs a source for the PR body template, read each time a
// PR is opened, so an edit to the conventions applies without a restart.
func (s *Service) SetPRBodyFunc(fn func() string) { s.prBodyFn = fn }

// SetPRTitleFunc installs the live PR title template source.
func (s *Service) SetPRTitleFunc(fn func() string) { s.prTitleFn = fn }

// SetReviewBranchFunc installs how a review's branch is named.
func (s *Service) SetReviewBranchFunc(fn func(id string) (string, error)) { s.reviewBranch = fn }

// SetGenerator installs what writes AI slots when a human opens a PR.
func (s *Service) SetGenerator(g conventions.Generator) { s.generate = g }

// SetPRVarsFunc installs the source of a branch's task placeholders.
func (s *Service) SetPRVarsFunc(fn func(branch string) map[string]string) { s.prVars = fn }

// SetPRBody installs the PR body template. Placeholders: {branch},
// {member}, {title}.
func (s *Service) SetPRBody(tpl string) { s.prBody = tpl }

// HasRunner reports whether diff production can work.
func (s *Service) HasRunner() bool { return s.runner != nil }

// CanDiff reports whether Diff can execute (runner or injected test seam).
func (s *Service) CanDiff() bool { return s.diffFn != nil }

// HasGH reports whether PR inputs and posting can work.
func (s *Service) HasGH() bool { return s.gh != nil && s.gh.Available() }

// ImportComments pulls the PR's review + issue comments through gh and
// merges them into the review's threads append-only: existing RemoteIDs
// are skipped, local drafts are never touched. Returns how many comments
// were newly mirrored.
func (s *Service) ImportComments(ctx context.Context, r Review) (int, error) {
	if !s.HasGH() {
		return 0, fmt.Errorf("review: gh unavailable — cannot import comments")
	}
	if r.Target.PRNumber <= 0 {
		return 0, fmt.Errorf("review: %s does not back a PR", r.ID)
	}
	mem, ok := s.ws.Member(r.Target.Member)
	if !ok {
		return 0, fmt.Errorf("review: unknown member %q", r.Target.Member)
	}
	repo := s.remoteURL(mem.Path)
	if repo == "" {
		return 0, fmt.Errorf("review: member %q has no origin remote", r.Target.Member)
	}
	num := fmt.Sprint(r.Target.PRNumber)
	rcs, err := s.gh.ReviewComments(ctx, repo, num)
	if err != nil {
		return 0, err
	}
	ics, err := s.gh.IssueComments(ctx, repo, num)
	if err != nil {
		return 0, err
	}
	all := append(append([]RemoteComment{}, rcs...), ics...)
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.Before(all[j].CreatedAt) })

	cur, ok := s.store.Get(r.ID)
	if !ok {
		return 0, fmt.Errorf("review: unknown review %q", r.ID)
	}
	seen := map[int64]bool{}
	threadByRoot := map[int64]int64{} // remote root → local thread id
	for _, t := range cur.Threads {
		if t.RemoteRoot != 0 {
			threadByRoot[t.RemoteRoot] = t.ID
		}
		for _, c := range t.Comments {
			if c.RemoteID != 0 {
				seen[c.RemoteID] = true
			}
		}
	}

	added := 0
	ensureThread := func(root int64, rc RemoteComment) (int64, error) {
		if id, okT := threadByRoot[root]; okT {
			return id, nil
		}
		file, line, side := "(remote)", 0, SideNew
		if rc.Path != "" {
			file = rc.Path
			side = SideNew
			line = rc.Line
			if rc.Side == "LEFT" && rc.OrigLine > 0 {
				line, side = rc.OrigLine, SideOld
			}
			if line == 0 && rc.OrigLine > 0 {
				line, side = rc.OrigLine, SideOld
			}
		}
		id, err := s.store.AddThread(r.ID, Thread{
			File: file, Line: line, Side: side, RemoteRoot: root,
		})
		if err != nil {
			return 0, err
		}
		threadByRoot[root] = id
		return id, nil
	}

	for _, rc := range all {
		if seen[rc.ID] || rc.Author == "" || strings.TrimSpace(rc.Body) == "" {
			continue
		}
		tid, terr := ensureThread(rc.RootID, rc)
		if terr != nil {
			return added, terr
		}
		if aerr := s.store.AppendComment(r.ID, tid, Comment{
			Author: rc.Author, Text: rc.Body,
			At: rc.CreatedAt, RemoteID: rc.ID,
		}); aerr != nil {
			return added, aerr
		}
		seen[rc.ID] = true
		added++
	}
	return added, nil
}

// PostComment publishes body on the PR backing this review through gh.
func (s *Service) PostComment(ctx context.Context, r Review, body string) error {
	if !s.HasGH() {
		return fmt.Errorf("review: gh unavailable — install the GitHub CLI to post")
	}
	mem, ok := s.ws.Member(r.Target.Member)
	if !ok {
		return fmt.Errorf("review: unknown member %q", r.Target.Member)
	}
	repo := s.remoteURL(mem.Path)
	if repo == "" {
		return fmt.Errorf("review: member %q has no origin remote", r.Target.Member)
	}
	if err := s.gh.PostComment(ctx, repo, fmt.Sprint(r.Target.PRNumber), body); err != nil {
		return fmt.Errorf("review: post comment: %w", err)
	}
	return s.store.MarkPosted(r.ID)
}

// pushBranch pushes refs/heads/<branch> from the member repo to origin.
func (s *Service) pushBranch(ctx context.Context, memberPath, branch string) error {
	repo, err := gitcore.Open(memberPath)
	if err != nil {
		return err
	}
	// The user's git pushes with their own ssh keys or credential helper
	// (F-112); DHI never handles a token for it.
	spec := "refs/heads/" + branch + ":refs/heads/" + branch
	if err := repo.Push(ctx, "", spec); err != nil {
		return err
	}
	return nil
}

// PRRequest is one PR to open. Title is the author's title, placed as
// {title} in the pr.title template. Fill holds AI-written slot text an
// agent already wrote; Generate lets DHI write missing slots (a human
// opened it), otherwise a missing slot is refused by name so the agent
// writes it.
type PRRequest struct {
	Member, Branch, Title, Base string
	Vars                        map[string]string // extra symbols (a review's id)
	Fill                        map[string]string
	Generate                    bool
}

// CreatePRForBranch pushes an existing branch of a member repo and opens
// a PR against base, DHI writing any AI slots. Used by the board and the
// Reviewer; agents go through OpenPR with their own fill.
func (s *Service) CreatePRForBranch(ctx context.Context, memberName, branch, title, base string) (PRMeta, error) {
	return s.OpenPR(ctx, PRRequest{Member: memberName, Branch: branch, Title: title, Base: base, Generate: true})
}

// OpenPR renders the title and body from the conventions, then pushes
// the branch and opens the PR. Nothing is pushed when rendering fails.
func (s *Service) OpenPR(ctx context.Context, req PRRequest) (PRMeta, error) {
	memberName, branch, base := req.Member, req.Branch, req.Base
	if !s.HasGH() {
		return PRMeta{}, fmt.Errorf("review: gh unavailable — install the GitHub CLI to create PRs")
	}
	mem, ok := s.ws.Member(memberName)
	if !ok {
		return PRMeta{}, fmt.Errorf("review: unknown member %q", memberName)
	}
	if branch == "" || base == "" {
		return PRMeta{}, fmt.Errorf("review: branch and base required")
	}
	worktreeDir := s.worktreeFor(memberName, branch)
	if worktreeDir != "" {
		wt, err := gitcore.Open(worktreeDir)
		if err == nil && wt.IsDirty() {
			return PRMeta{}, fmt.Errorf(
				"review: %s has uncommitted changes — commit them first (editor git panel)", branch)
		}
	}
	repoURL := s.remoteURL(mem.Path)
	if repoURL == "" {
		return PRMeta{}, fmt.Errorf("review: member %q has no origin remote", memberName)
	}
	title, body, err := s.RenderPR(ctx, req, mem.Path)
	if err != nil {
		return PRMeta{}, err
	}
	if err := s.pushBranch(ctx, mem.Path, branch); err != nil {
		return PRMeta{}, err
	}
	meta, err := s.gh.CreatePR(ctx, repoURL, title, body, base, branch)
	if err != nil {
		return PRMeta{}, err
	}
	return meta, nil
}

// RenderPR is the PR title and body the conventions give for req: every
// symbol filled, AI slots from req.Fill, then (Generate) from the
// generator. memberPath feeds the generator the branch's commits.
func (s *Service) RenderPR(ctx context.Context, req PRRequest, memberPath string) (title, body string, err error) {
	// F-029: no DHI footer crosses to the outside; the branch line is
	// context for the user's own PR.
	bodyTpl := s.prBody
	if s.prBodyFn != nil {
		bodyTpl = s.prBodyFn()
	}
	if bodyTpl == "" {
		bodyTpl = "Created from DHI worktree `{branch}`."
	}
	titleTpl := "{title}"
	if s.prTitleFn != nil {
		if t := strings.TrimSpace(s.prTitleFn()); t != "" {
			titleTpl = t
		}
	}
	vars := map[string]string{}
	if s.prVars != nil {
		for k, v := range s.prVars(req.Branch) {
			vars[k] = v
		}
	}
	for k, v := range req.Vars {
		vars[k] = v
	}
	vars["branch"], vars["member"], vars["title"] = req.Branch, req.Member, strings.TrimSpace(req.Title)
	fill := conventions.NormalizeFill(req.Fill)
	if fill == nil {
		fill = map[string]string{}
	}
	var missing []conventions.Slot
	for _, tpl := range []string{titleTpl, bodyTpl} {
		for _, sl := range conventions.Slots(tpl) {
			if strings.TrimSpace(fill[sl.Key]) == "" {
				missing = append(missing, sl)
			}
		}
	}
	if len(missing) > 0 && req.Generate {
		if s.generate == nil {
			return "", "", fmt.Errorf("review: the PR template has AI-written parts, but no agent can write them here")
		}
		got, gerr := s.generate(ctx, conventions.GenRequest{
			What:    "pull request",
			Shape:   "Title: " + conventions.Shape(titleTpl, vars) + "\n\n" + conventions.Shape(bodyTpl, vars),
			Slots:   dedupe(missing),
			Context: s.prContext(ctx, memberPath, req),
		})
		if gerr != nil {
			return "", "", fmt.Errorf("review: writing the PR text: %w", gerr)
		}
		for k, v := range conventions.NormalizeFill(got) {
			fill[k] = v
		}
	}
	if title, err = conventions.Render(titleTpl, vars, fill); err != nil {
		return "", "", fmt.Errorf("review: pr.title: %w", err)
	}
	if body, err = conventions.Render(bodyTpl, vars, fill); err != nil {
		return "", "", fmt.Errorf("review: pr.body: %w", err)
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return "", "", fmt.Errorf("review: the PR title is empty")
	}
	return title, body, nil
}

func dedupe(slots []conventions.Slot) []conventions.Slot {
	var out []conventions.Slot
	seen := map[string]bool{}
	for _, sl := range slots {
		if !seen[sl.Key] {
			seen[sl.Key] = true
			out = append(out, sl)
		}
	}
	return out
}

// prContext is what the generator reads to write PR text: the title,
// and the branch's commits and changed files against base (bounded).
func (s *Service) prContext(ctx context.Context, memberPath string, req PRRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nBranch: %s (into %s)\n", req.Title, req.Branch, req.Base)
	if s.diffFn != nil && memberPath != "" {
		rng := req.Base + ".." + req.Branch
		if log, err := s.diffFn(ctx, memberPath, "log", "--no-merges", "--format=- %s%n%b", rng); err == nil && strings.TrimSpace(log) != "" {
			b.WriteString("\nCommits:\n" + clip(log, 6000))
		}
		if stat, err := s.diffFn(ctx, memberPath, "diff", "--stat", rng); err == nil && strings.TrimSpace(stat) != "" {
			b.WriteString("\nChanged files:\n" + clip(stat, 3000))
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…"
}

// worktreeFor finds a checked-out worktree dir for branch within this
// workspace (member dirs + .dhi trees); "" when none matches.
func (s *Service) worktreeFor(memberName, branch string) string {
	mem, ok := s.ws.Member(memberName)
	if !ok {
		return ""
	}
	candidates := []string{mem.Path,
		filepath.Join(s.ws.Root, Dir)}
	var found string
	for _, root := range candidates {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			r, oerr := gitcore.Open(p)
			if oerr != nil {
				return nil
			}
			if b, berr := r.CurrentBranch(); berr == nil && b == branch {
				found = p
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return found
}

// CreatePR turns a started review into a full PR-backed review: pushes
// its review branch, opens the PR and links it back onto the card
// so diffs, invites, posting and comment-sync light up.
func (s *Service) CreatePR(ctx context.Context, id, title, base string) (Review, error) {
	r, ok := s.store.Get(id)
	if !ok {
		return Review{}, fmt.Errorf("review: unknown review %q", id)
	}
	if r.Done || r.WorkRel == "" {
		return Review{}, fmt.Errorf("review: %s has no live worktree", id)
	}
	if r.Target.PRNumber > 0 {
		return Review{}, fmt.Errorf("review: %s already backs PR #%d", id, r.Target.PRNumber)
	}
	// The worktree's own branch (branch.review may name it anything),
	// else the name the conventions give it.
	branch := ""
	if wt, oerr := gitcore.Open(filepath.Join(s.ws.Root, filepath.FromSlash(r.WorkRel))); oerr == nil {
		branch, _ = wt.CurrentBranch()
	}
	if branch == "" {
		branch = "review/" + id
		if s.reviewBranch != nil {
			b, berr := s.reviewBranch(id)
			if berr != nil {
				return Review{}, fmt.Errorf("review: %s: %w", id, berr)
			}
			branch = b
		}
	}
	meta, err := s.OpenPR(ctx, PRRequest{Member: r.Target.Member, Branch: branch, Title: title, Base: base,
		Vars: map[string]string{"id": id, "slug": id, "ticket": id}, Generate: true})
	if err != nil {
		return Review{}, err
	}
	err = s.store.mutate(id, func(r *Review) {
		r.Target.Kind = KindPR
		r.Target.PRNumber = meta.Number
		r.Target.Base = meta.BaseRef
		r.Target.HeadBranch = meta.HeadRef
		r.PRURL = meta.URL
	})
	if err != nil {
		return Review{}, err
	}
	out, _ := s.store.Get(id)
	return out, nil
}

var idCleanup = regexp.MustCompile(`[^a-z0-9._-]+`)

// makeID builds a slug id like "api-pr-42" or "api-branch-fix-auth".
// Collisions surface from Store.Create as a visible error.
func makeID(t Target) string {
	raw := t.Member + "-" + string(t.Kind)
	tail := t.Head
	if tail == "" {
		tail = fmt.Sprint(time.Now().Unix())
	}
	raw += "-" + tail
	raw = strings.ToLower(raw)
	raw = strings.TrimPrefix(raw, "refs/heads/")
	raw = strings.TrimPrefix(raw, "refs/dhi/pr/pr-")
	raw = strings.ReplaceAll(raw, "/", "-")
	return idCleanup.ReplaceAllString(raw, "-")
}

// Start creates a review session: resolves the head (fetching PR refs
// through go-git when kind==pr), creates the dedicated review worktree
// through the seam, and persists the card. The returned Review carries
// WorkRel and Channel.
func (s *Service) Start(ctx context.Context, memberName string, kind Kind, base, ref string, prNum int) (Review, error) {
	mem, ok := s.ws.Member(memberName)
	if !ok {
		return Review{}, fmt.Errorf("review: unknown member %q", memberName)
	}
	if !ValidKind(kind) {
		return Review{}, fmt.Errorf("review: bad kind %q", kind)
	}
	target := Target{Kind: kind, Member: mem.Name, Base: strings.TrimSpace(base), Head: ref, PRNumber: prNum}
	title := base + "..." + ref

	if kind == KindPR {
		if !s.HasGH() {
			return Review{}, fmt.Errorf("review: gh unavailable — PR reviews need the gh CLI on PATH")
		}
		repoURL := s.remoteURL(mem.Path)
		meta, err := s.gh.PR(ctx, repoURL, fmt.Sprint(prNum))
		if err != nil {
			return Review{}, err
		}
		sha, err := s.fetchPRHead(ctx, mem.Path, prNum)
		if err != nil {
			return Review{}, err
		}
		if target.Base == "" {
			target.Base = meta.BaseRef
		}
		target.Head = sha
		target.PRNumber = prNum
		title = meta.Title
	} else {
		if target.Base == "" {
			return Review{}, fmt.Errorf("review: base ref required")
		}
		if target.Head == "" {
			return Review{}, fmt.Errorf("review: branch/worktree reviews need a head ref")
		}
	}

	r := Review{
		ID:        makeID(target),
		Title:     title,
		Target:    target,
		Status:    Pending,
		Viewed:    map[string]bool{},
		Channel:   "#" + makeID(target),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	work, _ := s.store.seams()
	if work == nil {
		return Review{}, fmt.Errorf("review: worktree seam unavailable (is git installed?)")
	}
	rel, err := work(r.ID, mem.Name, target.Head)
	if err != nil {
		return Review{}, fmt.Errorf("review: create review worktree: %w", err)
	}
	r.WorkRel = rel

	if err := s.store.Create(r); err != nil {
		return Review{}, err
	}
	return r, nil
}

// Patch produces the raw unified patch text for a started review.
func (s *Service) Patch(ctx context.Context, r Review) (string, error) {
	if r.WorkRel == "" || r.Done {
		return "", fmt.Errorf("review: %s has no live worktree", r.ID)
	}
	if s.diffFn == nil {
		return "", fmt.Errorf("review: git is not installed — cannot diff %s", r.ID)
	}
	dir := filepath.Join(s.ws.Root, filepath.FromSlash(r.WorkRel))
	patch, err := s.diffFn(ctx, dir, "diff", "--no-color", r.Target.Base+"..."+r.Target.Head)
	if err != nil {
		return "", fmt.Errorf("review: diff %s: %w", r.ID, err)
	}
	return patch, nil
}

// Diff produces the parsed file model for a started review.
func (s *Service) Diff(ctx context.Context, r Review) ([]gitdiff.FileDiff, error) {
	patch, err := s.Patch(ctx, r)
	if err != nil {
		return nil, err
	}
	return gitdiff.Parse(patch), nil
}

// Discard removes the review worktree through the seam and marks the
// card done (history stays browsable).
func (s *Service) Discard(id string) error {
	_, discard := s.store.seams()
	r, ok := s.store.Get(id)
	if !ok {
		return fmt.Errorf("review: unknown review %q", id)
	}
	if discard == nil {
		return fmt.Errorf("review: worktree seam unavailable")
	}
	if r.WorkRel != "" && !r.Done {
		if err := discard(id, r.WorkRel); err != nil {
			return fmt.Errorf("review: discard %s: %w", id, err)
		}
	}
	return s.store.mutate(id, func(r *Review) { r.Done = true })
}

// ListBranches returns the member repo's local branches sorted.
func (s *Service) ListBranches(memberName string) ([]string, error) {
	mem, ok := s.ws.Member(memberName)
	if !ok {
		return nil, fmt.Errorf("review: unknown member %q", memberName)
	}
	repo, err := gitcore.Open(mem.Path)
	if err != nil {
		return nil, err
	}
	return repo.Branches()
}

// remoteURL reads origin's fetch URL (used to scope gh queries).
func (s *Service) remoteURL(memberPath string) string {
	repo, err := gitcore.Open(memberPath)
	if err != nil {
		return ""
	}
	return repo.RemoteURL("")
}

// fetchPRHead fetches refs/pull/N/head into a DHI-namespaced ref and
// returns the resolved commit.
func (s *Service) fetchPRHead(ctx context.Context, memberPath string, prNum int) (string, error) {
	repo, err := gitcore.Open(memberPath)
	if err != nil {
		return "", err
	}
	local := fmt.Sprintf("refs/dhi/pr/%d", prNum)
	spec := fmt.Sprintf("+refs/pull/%d/head:%s", prNum, local)
	sha, err := repo.Fetch(ctx, "", spec)
	if err != nil {
		return "", fmt.Errorf("review: fetch pull/%d: %w", prNum, err)
	}
	return sha, nil
}
