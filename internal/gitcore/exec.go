// GitRunner executes DHI's hermetic git CLI (ADR-0009): the binary is
// always the toolchain shim, the environment always comes from
// Manager.GitEnv, and there is no host-git fallback — a missing shim is
// an error callers surface visibly. The CLI covers what go-git cannot:
// the linked-worktree lifecycle today; further local plumbing later.
// Network transports are not built into the hermetic artifact, so
// clone/fetch/push stay in-process (ADR-0008).
package gitcore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/toolchain"
)

// defaultRunTimeout bounds every git invocation so a wedged process can
// never hang a turn or the UI forever.
const defaultRunTimeout = 2 * time.Minute

// Runner drives the hermetic git CLI.
type Runner struct {
	bin     string
	env     []string
	timeout time.Duration
}

// NewRunner binds a Runner to a git binary path plus its environment.
// Production callers pass toolchain.Manager.GitBin() and .GitEnv(nil);
// tests pass stubs.
func NewRunner(bin string, env []string) *Runner {
	return &Runner{bin: bin, env: env, timeout: defaultRunTimeout}
}

// ResolveRunner builds the production runner from the toolchain prefix,
// materializing the managed git config first. It fails when the shim
// has not been installed yet (degrade-visibly, ADR-0005/0009).
func ResolveRunner(m *toolchain.Manager) (*Runner, error) {
	bin := m.GitBin()
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("gitcore: hermetic git not installed at %s (run bootstrap)", bin)
	}
	if err := m.EnsureGitConfig(); err != nil {
		return nil, err
	}
	return NewRunner(bin, m.GitEnv(nil)), nil
}

// Run executes one git invocation inside dir ("" = inherit), returning
// stdout and stderr. A non-zero exit, launch failure, or timeout becomes
// an error carrying stderr for context.
func (r *Runner) Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir = dir
	cmd.Env = r.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out, errs := stdout.String(), stderr.String()
	if err != nil {
		detail := strings.TrimSpace(errs)
		if detail == "" {
			detail = err.Error()
		}
		return out, errs, fmt.Errorf("gitcore: git %s: %s",
			strings.Join(args, " "), detail)
	}
	return out, errs, nil
}

// Version reports the CLI's semantic version parsed from `git --version`.
func (r *Runner) Version(ctx context.Context) (string, error) {
	out, _, err := r.Run(ctx, "", "--version")
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(out)
	const prefix = "git version "
	if !strings.HasPrefix(s, prefix) {
		return "", fmt.Errorf("gitcore: unexpected --version output %q", s)
	}
	return strings.Fields(strings.TrimPrefix(s, prefix))[0], nil
}

// Worktree describes one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path     string // absolute path of the working tree
	Head     string // commit hex ("")
	Branch   string // short branch name ("" when detached/bare)
	Detached bool
	Bare     bool
}

// WorktreeAdd creates a linked worktree at path on a new branch,
// starting at startpoint ("HEAD" when empty). Mirrors
// `git worktree add -b <branch> <path> [<startpoint>]`.
func (r *Runner) WorktreeAdd(ctx context.Context, repo, path, branch, startpoint string) error {
	args := []string{"worktree", "add", "-b", branch, path}
	if startpoint != "" {
		args = append(args, startpoint)
	}
	_, _, err := r.Run(ctx, repo, args...)
	return err
}

// WorktreeRemove deletes the linked worktree at path. Refuses dirty
// trees unless force is set; callers confirm with the user first.
func (r *Runner) WorktreeRemove(ctx context.Context, repo, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, _, err := r.Run(ctx, repo, args...)
	return err
}

// Prune drops worktree metadata whose directories vanished.
func (r *Runner) Prune(ctx context.Context, repo string) error {
	_, _, err := r.Run(ctx, repo, "worktree", "prune")
	return err
}

// WorktreeList returns every linked worktree registered under repo,
// sorted by path for deterministic UI ordering.
func (r *Runner) WorktreeList(ctx context.Context, repo string) ([]Worktree, error) {
	out, _, err := r.Run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	wts := parseWorktreePorcelain(out)
	for i := 1; i < len(wts); i++ {
		for j := i; j > 0 && wts[j].Path < wts[j-1].Path; j-- {
			wts[j], wts[j-1] = wts[j-1], wts[j]
		}
	}
	return wts, nil
}

// parseWorktreePorcelain decodes v1 porcelain blocks separated by blank
// lines: worktree/HEAD lines plus optional branch|detached|bare flags.
func parseWorktreePorcelain(out string) []Worktree {
	var wts []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil {
			wts = append(wts, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			continue // header noise before the first block
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		}
	}
	flush()
	return wts
}
