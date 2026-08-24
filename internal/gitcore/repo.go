// Package gitcore is DHI's git service seam (ADR-0008): all repository
// operations run in-process over go-git — no managed git binary, no
// silent host-tool fallback. The editor's git view consumes this
// package only through its exported API.
package gitcore

import (
	"context"
	"fmt"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// FileStatus is one changed path. X is the index (staged) code, Y the
// worktree code; both use go-git status letters (M/A/D/R/U/?/C).
type FileStatus struct {
	Path   string
	X, Y   byte
	Staged bool // X not ' ' and not '?'
}

func (f FileStatus) WorktreeDirty() bool {
	return f.Y != ' ' && f.Y != 0
}

// CommitEntry is one log row.
type CommitEntry struct {
	Hash    string // full hex
	Short   string // 7 chars
	Message string // subject line
	Author  string
	When    time.Time
}

// Repo binds a repository on disk.
type Repo struct {
	path string
	r    *git.Repository
}

// Open loads the repository rooted at path.
func Open(path string) (*Repo, error) {
	if path == "" {
		return nil, fmt.Errorf("gitcore: empty path")
	}
	r, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("gitcore: open %s: %w", path, err)
	}
	return &Repo{path: path, r: r}, nil
}

// Path returns the repo root used to open it.
func (rp *Repo) Path() string { return rp.path }

// IsRepo reports whether dir contains a git repository.
func IsRepo(dir string) bool {
	_, err := git.PlainOpen(dir)
	return err == nil
}

// Status returns changed files sorted by path.
func (rp *Repo) Status() ([]FileStatus, error) {
	wt, err := rp.r.Worktree()
	if err != nil {
		return nil, fmt.Errorf("gitcore: worktree: %w", err)
	}
	st, err := wt.Status()
	if err != nil {
		return nil, fmt.Errorf("gitcore: status: %w", err)
	}
	out := make([]FileStatus, 0, len(st))
	for path, fs := range st {
		x, y := byte(fs.Staging), byte(fs.Worktree)
		out = append(out, FileStatus{
			Path:   path,
			X:      x,
			Y:      y,
			Staged: x != ' ' && x != '?' && x != 0,
		})
	}
	sortPaths(out)
	return out, nil
}

func sortPaths(s []FileStatus) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Path < s[j-1].Path; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Stage indexes the given paths ("." stages everything).
func (rp *Repo) Stage(paths ...string) error {
	wt, err := rp.r.Worktree()
	if err != nil {
		return fmt.Errorf("gitcore: worktree: %w", err)
	}
	if len(paths) == 1 && paths[0] == "." {
		if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
			return fmt.Errorf("gitcore: add all: %w", err)
		}
		return nil
	}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if err := wt.AddGlob(p); err == nil {
			continue
		}
		// deleted files can't be Added; remove them from index+worktree
		if _, rmErr := wt.Remove(p); rmErr != nil {
			return fmt.Errorf("gitcore: add %s: %w", p, err)
		}
	}
	return nil
}

// Unstage resets index entries for paths back to HEAD, keeping
// worktree content intact.
func (rp *Repo) Unstage(paths ...string) error {
	wt, err := rp.r.Worktree()
	if err != nil {
		return fmt.Errorf("gitcore: worktree: %w", err)
	}
	head, err := rp.headHash()
	if err != nil {
		return err
	}
	mode := git.MixedReset
	return wt.Reset(&git.ResetOptions{
		Commit: head,
		Files:  paths,
		Mode:   mode,
	})
}

func (rp *Repo) headHash() (plumbing.Hash, error) {
	ref, err := rp.r.Head()
	if err == plumbing.ErrReferenceNotFound {
		// unborn branch: reset against the empty tree
		return plumbing.Hash{}, nil
	}
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("gitcore: head: %w", err)
	}
	return ref.Hash(), nil
}

// CommitOptions configures a commit; Author is required because test
// and fresh repos have no configured identity.
type CommitOptions struct {
	Message string
	Author  string
	Email   string
}

// Commit commits everything currently staged.
func (rp *Repo) Commit(o CommitOptions) (string, error) {
	if strings.TrimSpace(o.Message) == "" {
		return "", fmt.Errorf("gitcore: empty commit message")
	}
	wt, err := rp.r.Worktree()
	if err != nil {
		return "", fmt.Errorf("gitcore: worktree: %w", err)
	}
	// go-git refuses when nothing is staged; surface that clearly
	st, err := wt.Status()
	if err != nil {
		return "", fmt.Errorf("gitcore: status: %w", err)
	}
	anyStaged := false
	for _, fs := range st {
		if fs.Staging != ' ' && fs.Staging != '?' && fs.Staging != 0 {
			anyStaged = true
			break
		}
	}
	if !anyStaged {
		return "", fmt.Errorf("gitcore: nothing staged to commit")
	}

	author := object.Signature{
		Name:  o.Author,
		Email: o.Email,
		When:  time.Now(),
	}
	h, err := wt.Commit(o.Message, &git.CommitOptions{
		Author:            &author,
		AllowEmptyCommits: false,
	})
	if err != nil {
		return "", fmt.Errorf("gitcore: commit: %w", err)
	}
	return h.String(), nil
}

// Log returns up to n most recent commits on HEAD.
func (rp *Repo) Log(n int) ([]CommitEntry, error) {
	if n < 1 {
		n = 20
	}
	from, err := rp.headHash()
	if err != nil {
		return nil, err
	}
	var iter object.CommitIter
	if from.IsZero() {
		iter, err = rp.r.Log(&git.LogOptions{})
	} else {
		iter, err = rp.r.Log(&git.LogOptions{From: from})
	}
	if err == plumbing.ErrReferenceNotFound || err == plumbing.ErrObjectNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitcore: log: %w", err)
	}
	defer iter.Close()
	var out []CommitEntry
	for i := 0; i < n; i++ {
		c, err := iter.Next()
		if err != nil {
			break
		}
		subject := c.Message
		if idx := strings.IndexByte(subject, '\n'); idx >= 0 {
			subject = subject[:idx]
		}
		out = append(out, CommitEntry{
			Hash:    c.Hash.String(),
			Short:   c.Hash.String()[:7],
			Message: strings.TrimSpace(subject),
			Author:  c.Author.Name,
			When:    c.Author.When,
		})
	}
	return out, nil
}

// CurrentBranch reports the checked-out branch name ("" when detached,
// "main-unborn" style name when no commit exists yet).
func (rp *Repo) CurrentBranch() (string, error) {
	ref, err := rp.r.Head()
	if err == plumbing.ErrReferenceNotFound {
		return "master (unborn)", nil
	}
	if err != nil {
		return "", fmt.Errorf("gitcore: head: %w", err)
	}
	if ref.Name().IsBranch() {
		return ref.Name().Short(), nil
	}
	return "", nil // detached HEAD
}

// Clone clones url (https or a local path) into dst and returns the
// opened repository. Network transports run in-process via go-git
// (ADR-0008/0009): public HTTPS repos work anonymously; credential
// prompts are never attempted — private repos surface an auth error
// visibly instead of hanging.
func Clone(ctx context.Context, url, dst string) (*Repo, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("gitcore: clone: empty url")
	}
	r, err := git.PlainCloneContext(ctx, dst, false, &git.CloneOptions{
		URL:      url,
		Progress: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("gitcore: clone %s: %w", url, err)
	}
	return &Repo{path: dst, r: r}, nil
}
