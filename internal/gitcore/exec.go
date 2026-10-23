// GitRunner executes the user's own git (F-112, ADR-0044): the git on
// their PATH, with their environment, so their config, includeIf, hooks,
// credential helpers and ssh-agent apply exactly as in their terminal.
// DHI adds only GIT_TERMINAL_PROMPT=0, so a background call with a
// missing credential fails visibly instead of waiting for input.
package gitcore

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// defaultRunTimeout bounds every git invocation so a wedged process can
// never hang a turn or the UI forever.
const defaultRunTimeout = 2 * time.Minute

// Runner drives the git CLI.
type Runner struct {
	bin     string
	env     []string
	timeout time.Duration
}

// NewRunner binds a Runner to a git binary path plus its environment.
// Production callers use ResolveRunner; tests pass stubs.
func NewRunner(bin string, env []string) *Runner {
	return &Runner{bin: bin, env: env, timeout: defaultRunTimeout}
}

// ResolveRunner builds the production runner: git as look finds it (the
// user's PATH), env as the user's shell has it. It fails, by name, when
// git is not installed.
func ResolveRunner(look func(string) (string, error), env []string) (*Runner, error) {
	bin, err := look("git")
	if err != nil {
		return nil, fmt.Errorf("gitcore: git is not installed (%v)", err)
	}
	return NewRunner(bin, BackgroundEnv(env)), nil
}

// BackgroundEnv is env for git calls nobody watches: never prompt.
func BackgroundEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") {
			out = append(out, kv)
		}
	}
	return append(out, "GIT_TERMINAL_PROMPT=0")
}

// networkTimeout bounds a clone, fetch or push (large repos, slow links).
const networkTimeout = 15 * time.Minute

// WithTimeout returns a copy of r with another per-call timeout.
func (r *Runner) WithTimeout(d time.Duration) *Runner {
	c := *r
	c.timeout = d
	return &c
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
	detach(cmd)
	// A killed shell leaves grandchildren holding our pipes; WaitDelay
	// forces Wait to return promptly once the context is done.
	cmd.WaitDelay = 500 * time.Millisecond
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

// Subjects returns up to n recent commit subjects in dir, newest first,
// merges excluded (F-082). An empty or missing repo yields none.
func (r *Runner) Subjects(ctx context.Context, dir string, n int) []string {
	out, _, err := r.Run(ctx, dir, "log", "--no-merges", "--format=%s", "-n", strconv.Itoa(n))
	if err != nil {
		return nil
	}
	var subs []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			subs = append(subs, l)
		}
	}
	return subs
}
