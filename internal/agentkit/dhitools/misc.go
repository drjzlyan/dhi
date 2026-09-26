package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// CommandRunner executes one already-vetted argv in dir. It is satisfied
// by a hermetic-env exec adapter wired in cmd/dhi; the allowlist policy
// lives here, never in the runner.
type CommandRunner interface {
	Run(ctx context.Context, dir string, argv []string) (string, error)
}

// runAllow is the fixed command allowlist for the `run` tool (F-030 P1):
// no free-form shell. A nil subcommand slice allows any args (a
// read-only tool). The per-agent/per-workflow list arrives with M16.
var runAllow = map[string][]string{
	"go": {"build", "test", "vet", "fmt"},
	"rg": nil,
}

type runPlan struct {
	argv []string
}

type askPlan struct {
	question string
}

// miscTools is the run + ask_human surface.
func (d Deps) miscTools() []tool {
	return []tool{
		{
			info: mcp.ToolInfo{
				Name:        "run",
				Description: "Run an allowlisted hermetic command (go build/test/vet/fmt, rg). No shell. Args: {\"program\":\"go\",\"args\":[\"test\",\"./...\"]}. Mutating: crosses approvals.",
				InputSchema: json.RawMessage(`{"type":"object","required":["program"],"properties":{"program":{"type":"string"},"args":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Program string   `json:"program"`
					Args    []string `json:"args"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				p := strings.TrimSpace(a.Program)
				if p == "" {
					return nil, fmt.Errorf("program is required")
				}
				allowed, ok := runAllow[p]
				if !ok {
					return nil, fmt.Errorf("program %q not allowed (allowed: %s)", p, strings.Join(runPrograms(), ", "))
				}
				if allowed != nil && len(a.Args) > 0 && !contains(allowed, a.Args[0]) {
					return nil, fmt.Errorf("subcommand %q not allowed for %s (allowed: %s)",
						a.Args[0], p, strings.Join(allowed, ", "))
				}
				return runPlan{argv: append([]string{p}, a.Args...)}, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				if d.Run == nil {
					return "", fmt.Errorf("command runner unavailable (run bootstrap)")
				}
				if strings.TrimSpace(d.Workdir) == "" {
					return "", fmt.Errorf("no working directory for this turn")
				}
				out, err := d.Run.Run(ctx, d.Workdir, dec.(runPlan).argv)
				if err != nil {
					// Preserve the command's own output for the agent.
					if strings.TrimSpace(out) != "" {
						return "", fmt.Errorf("%v\n%s", err, out)
					}
					return "", err
				}
				if strings.TrimSpace(out) == "" {
					return "(no output)\n", nil
				}
				return out, nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name:        "ask_human",
				Description: "Ask the human an open question in this turn's thread. Args: {\"question\": \"...\"}. The human answers next turn — this is not an approval.",
				InputSchema: json.RawMessage(`{"type":"object","required":["question"],"properties":{"question":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Question string `json:"question"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				q := strings.TrimSpace(a.Question)
				if q == "" {
					return nil, fmt.Errorf("question is required")
				}
				return askPlan{question: q}, nil
			},
			exec: func(_ context.Context, dec any) (string, error) {
				if d.Bus == nil || d.Channel == "" {
					return "", fmt.Errorf("no channel to ask in (this turn has no conversation)")
				}
				q := dec.(askPlan).question
				if _, err := d.Bus.Post(bus.Message{
					Channel: d.Channel, Thread: d.Thread, Author: d.Agent.ID, Text: q,
				}); err != nil {
					return "", err
				}
				return "asked the human in " + d.Channel, nil
			},
		},
	}
}

func runPrograms() []string {
	out := make([]string, 0, len(runAllow))
	for p := range runAllow {
		out = append(out, p)
	}
	// stable order
	if len(out) > 1 {
		for i := 1; i < len(out); i++ {
			for j := i; j > 0 && out[j] < out[j-1]; j-- {
				out[j], out[j-1] = out[j-1], out[j]
			}
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
