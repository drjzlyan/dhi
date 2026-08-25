package review

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

// HasRunner reports whether diff production can work.
func (s *Service) HasRunner() bool { return s.runner != nil }

// CanDiff reports whether Diff can execute (runner or injected test seam).
func (s *Service) CanDiff() bool { return s.diffFn != nil }

// HasGH reports whether PR inputs and posting can work.
func (s *Service) HasGH() bool { return s.gh != nil && s.gh.Available() }

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

// Diff produces the parsed file model for a started review by running
// the hermetic git diff inside its review worktree.
func (s *Service) Diff(ctx context.Context, r Review) ([]gitdiff.FileDiff, error) {
	if r.WorkRel == "" || r.Done {
		return nil, fmt.Errorf("review: %s has no live worktree", r.ID)
	}
	if s.diffFn == nil {
		return nil, fmt.Errorf("review: hermetic git unavailable — cannot diff %s", r.ID)
	}
	dir := filepath.Join(s.ws.Root, filepath.FromSlash(r.WorkRel))
	patch, err := s.diffFn(ctx, dir, "diff", "--no-color", r.Target.Base+"..."+r.Target.Head)
	if err != nil {
		return nil, fmt.Errorf("review: diff %s: %w", r.ID, err)
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
