package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// runAllowed checks an argv against the `run` allowlist (program and
// first argument) — the same rule for agent-made recipes as for `run`.
func runAllowed(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty command")
	}
	allowed, ok := runAllow[argv[0]]
	if !ok {
		return fmt.Errorf("program %q not allowed (allowed: %s)", argv[0], strings.Join(runPrograms(), ", "))
	}
	if allowed != nil && len(argv) > 1 && !contains(allowed, argv[1]) {
		return fmt.Errorf("subcommand %q not allowed for %s (allowed: %s)", argv[1], argv[0], strings.Join(allowed, ", "))
	}
	return nil
}

// forgeTools serves tool_propose (every agent) and every approved forged
// tool as custom__<name> (F-069).
func (d Deps) forgeTools() []tool {
	if d.Forge == nil {
		return nil
	}
	out := []tool{d.toolPropose()}
	active, err := d.Forge.Active()
	if err != nil {
		return out
	}
	for _, ft := range active {
		out = append(out, d.customTool(ft))
	}
	return out
}

// customScope is the declared scope of a served forged tool, if name is
// one (unknown tools stay admin/deny in scopes.ToolScope).
func (d Deps) customScope(name string) (scopes.Scope, bool) {
	if d.Forge == nil || !strings.HasPrefix(name, forge.Prefix) {
		return "", false
	}
	active, err := d.Forge.Active()
	if err != nil {
		return "", false
	}
	for _, ft := range active {
		if forge.Prefix+ft.Name == name {
			sc, err := scopes.ParseScope(ft.Scope)
			return sc, err == nil
		}
	}
	return "", false
}

func (d Deps) customTool(ft forge.Tool) tool {
	return tool{
		info: mcp.ToolInfo{
			Name:        forge.Prefix + ft.Name,
			Description: fmt.Sprintf("%s (forged by %s, v%d, %s scope)", ft.Description, ft.ProposedBy, ft.Version, ft.Scope),
			InputSchema: json.RawMessage(ft.InputSchema()),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var m map[string]any
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &m); err != nil {
					return nil, fmt.Errorf("arguments: %w", err)
				}
			}
			return ft.Args(m)
		},
		exec: func(ctx context.Context, dec any) (string, error) {
			if d.Run == nil {
				return "", fmt.Errorf("command runner unavailable (run bootstrap)")
			}
			if strings.TrimSpace(d.Workdir) == "" {
				return "", fmt.Errorf("no working directory for this turn")
			}
			args := dec.(map[string]string)
			defer d.Forge.RecordUse(ft.Name)
			if ft.Script != "" {
				argv := append([]string{d.Forge.ScriptPath(ft.Name)}, ft.ScriptArgs(args)...)
				out, err := d.Run.Run(ctx, d.Workdir, argv, false)
				if err != nil {
					return "", fmt.Errorf("%v\n%s", err, out)
				}
				return orNoOutput(out), nil
			}
			var b strings.Builder
			for _, argv := range ft.Expand(args) {
				if err := runAllowed(argv); err != nil {
					return "", fmt.Errorf("step %q refused: %w", strings.Join(argv, " "), err)
				}
				out, err := d.Run.Run(ctx, d.Workdir, argv, false)
				fmt.Fprintf(&b, "$ %s\n%s", strings.Join(argv, " "), orNoOutput(out))
				if err != nil {
					return "", fmt.Errorf("%v\n%s", err, b.String())
				}
			}
			return b.String(), nil
		},
	}
}

func orNoOutput(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no output)\n"
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// toolPropose lets an agent turn repeated work into a tool. The proposal
// waits for the human (Settings → PROPOSALS); the agent's @you mention
// puts it in the Inbox.
func (d Deps) toolPropose() tool {
	return tool{
		info: mcp.ToolInfo{
			Name: "tool_propose",
			Description: "Propose a reusable tool when you notice you keep running the same commands. The human approves it before " +
				"anyone can use it; then every agent can call it as custom__<name>. Two kinds: a recipe of allowlisted commands " +
				"(go build/test/vet/fmt, rg — no shell) whose args may use {{input}} templates, or a script (#! line, scope exec). " +
				"Scope is read, write, exec or git. Example: {\"name\":\"race_test\",\"description\":\"Run one package's tests with the race detector\"," +
				"\"scope\":\"exec\",\"inputs\":[{\"name\":\"pkg\",\"type\":\"string\",\"required\":true}],\"steps\":[[\"go\",\"test\",\"-race\",\"{{pkg}}\"]]," +
				"\"rationale\":\"I ran this in 4 of my last 6 tasks\"}",
			InputSchema: json.RawMessage(`{"type":"object","required":["name","description","scope","rationale"],"properties":{` +
				`"name":{"type":"string"},"description":{"type":"string"},"scope":{"type":"string","enum":["read","write","exec","git"]},` +
				`"inputs":{"type":"array","items":{"type":"object","required":["name","type"],"properties":{"name":{"type":"string"},"type":{"type":"string","enum":["string","integer","boolean"]},"description":{"type":"string"},"required":{"type":"boolean"}},"additionalProperties":false}},` +
				`"steps":{"type":"array","items":{"type":"array","items":{"type":"string"}}},"script":{"type":"string"},"rationale":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Name        string        `json:"name"`
				Description string        `json:"description"`
				Scope       string        `json:"scope"`
				Inputs      []forge.Input `json:"inputs"`
				Steps       [][]string    `json:"steps"`
				Script      string        `json:"script"`
				Rationale   string        `json:"rationale"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			t := forge.Tool{
				Name: strings.TrimSpace(a.Name), Description: strings.TrimSpace(a.Description), Scope: a.Scope,
				Inputs: a.Inputs, Script: a.Script, Rationale: strings.TrimSpace(a.Rationale),
			}
			for _, st := range a.Steps {
				t.Steps = append(t.Steps, forge.Step{Run: st})
			}
			if err := t.Validate(runAllowed); err != nil {
				return nil, err
			}
			return t, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			t := dec.(forge.Tool)
			t.ProposedBy = d.Agent.ID
			p, err := d.Forge.Propose(t, runAllowed)
			if err != nil {
				return "", err
			}
			if d.Bus != nil && d.Channel != "" {
				_, _ = d.Bus.Post(bus.Message{
					Channel: d.Channel, Thread: d.Thread, Author: d.Agent.ID,
					Text: fmt.Sprintf("@%s I proposed a tool `%s` (v%d, %s): %s — review it in Settings → PROPOSALS",
						bus.Human, p.Name, p.Version, p.Scope, p.Description),
				})
			}
			return fmt.Sprintf("proposed %s v%d — it becomes custom__%s once the human approves it", p.Name, p.Version, p.Name), nil
		},
	}
}
