package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/tools"
)

// SecretStore resolves a secret for a workspace root: its own first, then
// the global one (credstore.Store, F-089).
type SecretStore interface {
	LookupIn(root, name string) (string, bool)
}

// secretSeam backs secret_request (F-089). A stored secret the agent was
// already granted is reported available; otherwise a card asks the human,
// who types a missing value into a masked field. The value is never
// returned: the agent uses $NAME through DHI's run tool.
func (r *Runtime) secretSeam(agentID string) func(ctx context.Context, name, reason string) (string, error) {
	if r.cfg.Approvals == nil || r.cfg.Secrets == nil {
		return nil
	}
	return func(ctx context.Context, name, reason string) (string, error) {
		root := r.cfg.WS.Root
		_, have := r.cfg.Secrets.LookupIn(root, name)
		avail := "$" + name + " is set for you in DHI's run tool (not your own shell); its value never shows in output."
		if have && r.cfg.Approvals.Granted(agentID, tools.AccessSecret+name) {
			return avail, nil
		}
		if err := r.cfg.Approvals.AskSecret(ctx, agentID, name, reason, !have); err != nil {
			return "", err
		}
		if _, ok := r.cfg.Secrets.LookupIn(root, name); !ok {
			return "", fmt.Errorf("allowed, but no value for %s was saved; ask again when it is ready", name)
		}
		return "granted. " + avail, nil
	}
}

// grantedSecrets lists the names agent may use that resolve here.
func (r *Runtime) grantedSecrets(agentID string) []string {
	if r.cfg.Approvals == nil || r.cfg.Secrets == nil {
		return nil
	}
	var out []string
	for _, g := range r.cfg.Approvals.Grants() {
		name, ok := strings.CutPrefix(g.Scope, tools.AccessSecret)
		if !ok || g.Agent != agentID {
			continue
		}
		if _, ok := r.cfg.Secrets.LookupIn(r.cfg.WS.Root, name); ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// secretEnv is NAME=value for each granted secret, read at call time so
// a rotation or revoke applies to the next run.
func (r *Runtime) secretEnv(agentID string) func() []string {
	if r.cfg.Secrets == nil {
		return nil
	}
	return func() []string {
		var env []string
		for _, n := range r.grantedSecrets(agentID) {
			if v, ok := r.cfg.Secrets.LookupIn(r.cfg.WS.Root, n); ok {
				env = append(env, n+"="+v)
			}
		}
		return env
	}
}

// secretsLine tells the agent which secrets it can use, by name only.
func (r *Runtime) secretsLine(agentID string) string {
	line := "\nNeed a credential? Call secret_request with its env-var name; never ask for one in chat."
	if names := r.grantedSecrets(agentID); len(names) > 0 {
		line += " Already set for you in DHI's run tool: $" + strings.Join(names, ", $") + "."
	}
	return line
}

// skillSuggest binds the suggesting agent to the skill_suggest seam.
func (r *Runtime) skillSuggest(agentID string) func(ctx context.Context, entry, reason string) (string, error) {
	if r.cfg.SkillSuggest == nil {
		return nil
	}
	return func(ctx context.Context, entry, reason string) (string, error) {
		return r.cfg.SkillSuggest(ctx, agentID, entry, reason)
	}
}

// wsRoot is the workspace root ("" without one), whose own secrets come
// before the global ones.
func (r *Runtime) wsRoot() string {
	if r.cfg.WS == nil {
		return ""
	}
	return r.cfg.WS.Root
}
