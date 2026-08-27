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

	"github.com/go-git/go-git/v5/plumbing/transport"
	httptransport "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Service orchestrates review sessions over member repos: it resolves
// targets (fetching PR heads via go-git), creates the dedicated review
// worktree through the store's seam, and produces diffs with the
// hermetic git runner. Persistence lives in Store; this type holds no
// state beyond its deps.
type Service struct {
	ws     *workspace.Workspace
	store  *Store
	runner *gitcore.Runner // nil = diff production unavailable (visible)
	gh     GH              // nil = PR inputs unavailable (visible)

	// diffFn is injectable for tests; production wraps runner.Run.
	diffFn func(ctx context.Context, dir string, args ...string) (string, error)
	// tokenFn mints push credentials (gh auth token in production).
	tokenFn func(ctx context.Context) (string, error)
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

// SetTokenFn installs the credential source for pushes (production:
// gh auth token; tests: nil keeps local remotes auth-free).
func (s *Service) SetTokenFn(fn func(ctx context.Context) (string, error)) {
	s.tokenFn = fn
}

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

// pushAuth builds go-git credentials from the injected token source.
// HTTPS origins get BasicAuth; anything else pushes anonymously (local
// test remotes) or fails visibly upstream.
func (s *Service) pushAuth(ctx context.Context) (transport.AuthMethod, error) {
	if s.tokenFn == nil {
		return nil, nil
	}
	tok, err := s.tokenFn(ctx)
	if err != nil || tok == "" {
		return nil, err
	}
	return &httptransport.BasicAuth{Username: "x-access-token", Password: tok}, nil
}

// pushBranch pushes refs/heads/<branch> from the member repo to origin.
func (s *Service) pushBranch(ctx context.Context, memberPath, branch string) error {
	repo, err := gitcore.Open(memberPath)
	if err != nil {
		return err
	}
	auth, err := s.pushAuth(ctx)
	if err != nil {
		return fmt.Errorf("review: push credentials: %w", err)
	}
	spec := "refs/heads/" + branch + ":refs/heads/" + branch
	if err := repo.Push(ctx, "", spec, auth); err != nil {
		return err
	}
	return nil
}

var prCreateTimeout = 5 * time.Minute

// CreatePRForBranch pushes an existing branch of a member repo and opens
// a PR against base. Used by both the Reviewer (review/<id> branches)
// and task cards (task/<slug> branches).
func (s *Service) CreatePRForBranch(ctx context.Context, memberName, branch, title, base string) (PRMeta, error) {
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
	if err := s.pushBranch(ctx, mem.Path, branch); err != nil {
		return PRMeta{}, err
	}
	body := fmt.Sprintf("Created from DHI worktree `%s`.\n\n_Reviewed with DHI's Reviewer floor._", branch)
	meta, err := s.gh.CreatePR(ctx, repoURL, title, body, base, branch)
	if err != nil {
		return PRMeta{}, err
	}
	return meta, nil
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
// its review/<id> branch, opens the PR and links it back onto the card
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
	meta, err := s.CreatePRForBranch(ctx, r.Target.Member, "review/"+id, title, base)
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
		return Review{}, fmt.Errorf("review: worktree seam unavailable (hermetic git not installed?)")
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
		return "", fmt.Errorf("review: hermetic git unavailable — cannot diff %s", r.ID)
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
