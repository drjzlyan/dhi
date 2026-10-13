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
	if r.cfg.Approvals == nil {
		return sb
	}
	roots := r.cfg.Approvals.Targets(e.m.ID)
	if len(roots) == 0 {
		return sb
	}
	re, ok := sb.(sandbox.RootExtender)
	if !ok {
		return sb
	}
	ext, err := re.WithExtraRoots(roots)
	if err != nil {
		return sb
	}
	return ext
}

// accessSeam backs permission_request (F-087). Write access parks an
// approval card for the path; once granted, the turn continues in a
// fresh run with the path in its sandbox. Network and exec reuse the
// capability scopes (checked per call, so they apply at once).
func (r *Runtime) accessSeam(agentID string, trigger bus.Message, rl *relaunch) func(ctx context.Context, kind, target, reason string) (string, error) {
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
		case "network", "exec":
			if err := r.cfg.Approvals.AskScope(ctx, agentID, kind, sandbox.OpExec, kind+" "+strings.TrimSpace(target), reason); err != nil {
				return "", err
			}
			return kind + " is allowed; carry on", nil
		default:
			return "", fmt.Errorf("kind must be write, network or exec")
		}
	}
}

func (r *Runtime) writeGranted(agentID, p string) bool {
	for _, g := range r.cfg.Approvals.Targets(agentID) {
		if within(p, g) {
			return true
		}
	}
	return false
}

// offerAccess turns a sandbox write denial seen in a run into the same
// approval card (F-087), so the human can unblock an agent that never
// thought to ask. One card per agent and path at a time; a grant wakes
// the agent in the thread to retry.
func (r *Runtime) offerAccess(agentID string, trigger bus.Message, denied string) {
	if r.cfg.Approvals == nil {
		return
	}
	p, err := r.accessTarget(denied)
	if err != nil || r.writeGranted(agentID, p) {
		return
	}
	key := agentID + "|" + p
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
		err := r.cfg.Approvals.AskScope(context.Background(), agentID, tools.AccessWrite+p, sandbox.OpWrite, p,
			"the sandbox blocked "+agentID+" from writing here")
		if err == nil && r.writeGranted(agentID, p) {
			r.continueTurn(context.Background(), agentID, trigger,
				"write access to "+p+" was granted; retry what the sandbox blocked")
		}
	}()
}
