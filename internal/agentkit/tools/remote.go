package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

// ToolCaller is the transport-neutral surface of a connected tool
// server (satisfied by *mcp.Stdio and *mcp.HTTP without importing them).
type ToolCaller interface {
	CallTool(ctx context.Context, name string, args json.RawMessage) (content string, isError bool, err error)
}

// RemoteInfo mirrors one server-side tool description.
type RemoteInfo struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Gate authorizes one operation class against a display target before a
// tool executes. Remote calls have no path scope, so they are gated as
// workspace-wide net operations.
type Gate func(ctx context.Context, op sandbox.Op, target string) error

// PolicyGate builds a Gate consulting p directly (not Guard.Check,
// which requires a jail-mapped path), routing Ask through approvals.
func PolicyGate(p *sandbox.Policy, ap *Approvals, agentID string) Gate {
	return func(ctx context.Context, op sandbox.Op, target string) error {
		dec := p.Evaluate(op, "")
		switch dec.Effect {
		case sandbox.Allow:
			return nil
		case sandbox.Ask:
			if ap == nil {
				return fmt.Errorf("denied: %s (no approval channel configured)", dec.Reason)
			}
			return ap.wait(ctx, agentID, op, target, dec.Reason)
		default:
			return fmt.Errorf("denied: %s", dec.Reason)
		}
	}
}

var unsafeRe = regexp.MustCompile(`[^a-z0-9_]+`)

// MCPToolName namespaces a server tool as mcp__<server>__<tool>, matching
// the reference grammar manifests validate against.
func MCPToolName(server, tool string) string {
	clean := func(s string) string {
		return strings.Trim(unsafeRe.ReplaceAllString(strings.ToLower(s), "_"), "_")
	}
	return fmt.Sprintf("mcp__%s__%s", clean(server), clean(tool))
}

type remoteTool struct {
	gate   Gate
	caller ToolCaller
	info   RemoteInfo
	ref    string // namespaced registry name
}

// RemoteTools renders every tool of one server as a registry Tool under
// its namespaced reference.
func RemoteTools(server string, gate Gate, c ToolCaller, infos []RemoteInfo) []Tool {
	out := make([]Tool, 0, len(infos))
	for _, info := range infos {
		out = append(out, remoteTool{gate: gate, caller: c, info: info, ref: MCPToolName(server, info.Name)})
	}
	return out
}

func (t remoteTool) Def() Def {
	return Def{
		Name:        t.ref,
		Description: t.info.Description,
		InputSchema: string(t.info.Schema),
	}
}

func (t remoteTool) Exec(ctx context.Context, input json.RawMessage) (string, error) {
	if input == nil {
		input = json.RawMessage(`{}`)
	}
	if err := t.gate(ctx, sandbox.OpNet, t.ref); err != nil {
		return "", fmt.Errorf("%s: %w", t.ref, err)
	}
	content, isErr, err := t.caller.CallTool(ctx, t.info.Name, input)
	if err != nil {
		return "", fmt.Errorf("%s: %w", t.ref, err)
	}
	if isErr {
		return "", fmt.Errorf("%s: %s", t.ref, content)
	}
	return content, nil
}
