package runtime

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/credstore"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// accessTarget normalizes a path an agent wants to write beyond its
// sandbox roots (F-087): absolute, ~ expanded, symlinks of its existing
// part resolved (the sandbox matches real paths: /tmp is /private/tmp).
// DHI's control files, its user config and the credential trees are
// never grantable, and neither is a root as broad as / or your home.
func (r *Runtime) accessTarget(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("target must be an absolute path (got %q)", raw)
	}
	p = realPath(filepath.Clean(p))
	home, _ := os.UserHomeDir()
	if p == "/" || (home != "" && p == realPath(home)) {
		return "", fmt.Errorf("%s is too broad to grant; ask for the folder you need", p)
	}
	for _, g := range r.ungrantable() {
		if within(p, g) || within(g, p) {
			return "", fmt.Errorf("%s is off limits to agents (it holds or contains %s)", p, g)
		}
	}
	return p, nil
}

// ungrantable are the trees no access grant may reach: DHI's control
// files (ADR-0035 — the reopened task and session trees are already
// writable), DHI's user config (credentials, grants) and the credential
// directories the sandbox denies reads of.
func (r *Runtime) ungrantable() []string {
	out := []string{realPath(filepath.Join(r.cfg.WS.Root, workspace.DHIDir))}
	if cred, err := credstore.DefaultPath(); err == nil {
		out = append(out, realPath(filepath.Dir(cred)))
	}
	for _, c := range sandbox.CredentialPaths() {
		out = append(out, realPath(c))
	}
	return out
}

// realPath resolves symlinks along the longest existing prefix of p.
func realPath(p string) string {
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return p
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func within(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// spawnSandbox is the agent's sandbox for one spawn: the runtime's,
// grown by the write grants the human gave this agent (F-087). A grant
// given during a run reaches the next spawn — the profile is fixed when
// the CLI starts.
func (r *Runtime) spawnSandbox(e *entry) sandbox.Sandbox {
	sb := e.guard.Sandbox
	// The folders on the current PATH run, a reloaded one included (F-109).
	if r.cfg.CLIEnvFunc != nil {
		home, _ := os.UserHomeDir()
		if roots := sandbox.ExecRoots(envPath(r.baseEnv()), home); len(roots) > 0 {
			if re, ok := sb.(sandbox.ReadOnlyExtender); ok {
				if ext, err := re.WithExtraReadOnly(roots); err == nil {
					sb = ext
				}
			}
		}
	}
	if r.cfg.Approvals == nil {
		return sb
	}
	if roots := r.cfg.Approvals.Targets(e.m.ID); len(roots) > 0 {
		if re, ok := sb.(sandbox.RootExtender); ok {
			if ext, err := re.WithExtraRoots(roots); err == nil {
				sb = ext
			}
		}
	}
	if ro := r.cfg.Approvals.ExecTargets(e.m.ID); len(ro) > 0 {
		if re, ok := sb.(sandbox.ReadOnlyExtender); ok {
			if ext, err := re.WithExtraReadOnly(ro); err == nil {
				sb = ext
			}
		}
	}
	return sb
}

// accessSeam backs permission_request (F-087). Write access parks an
// approval card for the path; once granted, the turn continues in a
// fresh run with the path in its sandbox. Network and exec reuse the
// capability scopes (checked per call, so they apply at once).
func (r *Runtime) accessSeam(agentID string, rl *relaunch) func(ctx context.Context, kind, target, reason string) (string, error) {
	if r.cfg.Approvals == nil {
		return nil
	}
	return func(ctx context.Context, kind, target, reason string) (string, error) {
		switch kind {
		case "write":
			p, err := r.accessTarget(target)
			if err != nil {
				return "", err
			}
			for _, g := range r.cfg.Approvals.Targets(agentID) {
				if within(p, g) {
					return "write access to " + p + " is already granted; it applies from your next run", nil
				}
			}
			if err := r.cfg.Approvals.AskScope(ctx, agentID, tools.AccessWrite+p, sandbox.OpWrite, p, reason); err != nil {
				return "", err
			}
			if !r.writeGranted(agentID, p) {
				return "allowed for this call only, which cannot widen a running sandbox; ask again if you still need it", nil
			}
			why := "write access to " + p + " was granted."
			rl.set(why)
			return why + " End this run now with one short line; DHI continues you with the access.", nil
		case "exec":
			if filepath.IsAbs(strings.TrimSpace(target)) || strings.HasPrefix(strings.TrimSpace(target), "~") {
				return r.askExecRoot(ctx, agentID, target, reason, rl)
			}
			fallthrough
		case "network":
			if err := r.cfg.Approvals.AskScope(ctx, agentID, kind, sandbox.OpExec, kind+" "+strings.TrimSpace(target), reason); err != nil {
				return "", err
			}
			return kind + " is allowed; carry on", nil
		default:
			return "", fmt.Errorf("kind must be write, network or exec")
		}
	}
}

// askExecRoot asks to run programs from the folder holding target (a
// binary or a dir outside the sandbox's exec roots, e.g. a tool installed
// with Homebrew). Read and execute only; applies from the next spawn.
func (r *Runtime) askExecRoot(ctx context.Context, agentID, target, reason string, rl *relaunch) (string, error) {
	p, err := r.accessTarget(target)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		p = filepath.Dir(p)
	}
	for _, g := range r.cfg.Approvals.ExecTargets(agentID) {
		if within(p, g) {
			return "running programs from " + p + " is already granted; it applies from your next run", nil
		}
	}
	if err := r.cfg.Approvals.AskScope(ctx, agentID, tools.AccessExec+p, sandbox.OpExec, p, reason); err != nil {
		return "", err
	}
	why := "running programs from " + p + " was granted."
	rl.set(why)
	return why + " DHI continues you in a fresh run with it.", nil
}

func (r *Runtime) writeGranted(agentID, p string) bool {
	for _, g := range r.cfg.Approvals.Targets(agentID) {
		if within(p, g) {
			return true
		}
	}
	return false
}

// offerAccess turns a sandbox denial seen in a run into the same
// approval card (F-087, F-113): a write card for a path, or — when the
// denial names a program — a card to run programs from its folder. One
// card per agent and target at a time; a grant wakes the agent in the
// thread to retry.
func (r *Runtime) offerAccess(agentID string, trigger bus.Message, denied string) {
	if r.cfg.Approvals == nil {
		return
	}
	p, err := r.accessTarget(denied)
	if err != nil {
		return
	}
	scope, op, what, done := tools.AccessWrite+p, sandbox.OpWrite, "write", "write access to "+p
	granted := func() bool { return r.writeGranted(agentID, p) }
	if clirun.IsProgram(p) {
		p = filepath.Dir(p)
		scope, op, what, done = tools.AccessExec+p, sandbox.OpExec, "run programs from", "running programs from "+p
		granted = func() bool {
			for _, g := range r.cfg.Approvals.ExecTargets(agentID) {
				if within(p, g) {
					return true
				}
			}
			return false
		}
	}
	if granted() {
		return
	}
	key := agentID + "|" + scope
	r.offerMu.Lock()
	if r.offering == nil {
		r.offering = map[string]bool{}
	}
	if r.offering[key] {
		r.offerMu.Unlock()
		return
	}
	r.offering[key] = true
	r.offerMu.Unlock()
	// Not tracked by WaitBackground: the card may wait on the human
	// indefinitely; the continuation it leads to is tracked.
	go func() {
		defer func() {
			r.offerMu.Lock()
			delete(r.offering, key)
			r.offerMu.Unlock()
		}()
		err := r.cfg.Approvals.AskScope(context.Background(), agentID, scope, op, p,
			"the sandbox blocked "+agentID+" — it needs to "+what+" "+p)
		if err == nil && granted() {
			r.continueTurn(context.Background(), agentID, trigger,
				done+" was granted; retry what the sandbox blocked")
		}
	}()
}

// runFor is the command runner for e's `run` and skill_run calls: the
// runtime's, grown by the write and exec grants e holds (F-113), so a
// folder the human allowed is reachable from run too, not only from the
// agent CLI's own tools.
func (r *Runtime) runFor(e *entry) dhitools.CommandRunner {
	gr, ok := r.cfg.Run.(dhitools.GrantRunner)
	if !ok || r.cfg.Approvals == nil {
		return r.cfg.Run
	}
	write, exec := r.cfg.Approvals.Targets(e.m.ID), r.cfg.Approvals.ExecTargets(e.m.ID)
	if len(write) == 0 && len(exec) == 0 {
		return r.cfg.Run
	}
	return gr.WithGrants(write, exec)
}
