package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// CommandRunner executes one already-vetted argv in dir. It is satisfied
// by an exec adapter wired in cmd/dhi; the allowlist policy
// lives here, never in the runner.
type CommandRunner interface {
	Run(ctx context.Context, dir string, argv []string, allowNetwork bool) (string, error)
}

// GrantRunner is a CommandRunner whose sandbox can grow by the access
// grants the human gave one agent (F-113): write roots and folders to run
// programs from. The runtime asks it for a per-agent runner each turn.
type GrantRunner interface {
	WithGrants(write, exec []string) CommandRunner
}

// EnvRunner is a CommandRunner that can add environment to one run: the
// secrets an agent was granted (F-089). They reach that command only —
// never the agent CLI, whose own shell could print them into its session.
type EnvRunner interface {
	RunEnv(ctx context.Context, dir string, argv []string, allowNetwork bool, extra []string) (string, error)
}

// networkAllowed reports whether a command may reach the network: the
// agent's network scope is auto, or it asks and the human granted it
// (permission_request "network", F-113).
func (d Deps) networkAllowed() bool {
	set := d.Scopes
	if set == nil {
		set = scopes.Default()
	}
	switch set.EffectFor(scopes.Network) {
	case scopes.Auto:
		return true
	case scopes.Ask:
		return d.Approvals != nil && d.Agent != nil && d.Approvals.Granted(d.Agent.ID, string(scopes.Network))
	}
	return false
}

// runWith runs argv with the agent's granted secrets in its environment.
func (d Deps) runWith(ctx context.Context, argv []string, allowNet bool) (string, error) {
	var extra []string
	if d.SecretEnv != nil {
		extra = d.SecretEnv()
	}
	if len(extra) > 0 {
		if er, ok := d.Run.(EnvRunner); ok {
			return er.RunEnv(ctx, d.Workdir, argv, allowNet, extra)
		}
		return "", fmt.Errorf("this runner cannot pass your granted secrets; nothing was run")
	}
	return d.Run.Run(ctx, d.Workdir, argv, allowNet)
}

// runAllow is the fixed command allowlist for the `run` tool (F-030 P1):
// no free-form shell. A nil subcommand slice allows any args (a
// read-only tool). The per-agent/per-workflow list arrives with M16.
var runAllow = map[string][]string{
	"go": {"build", "test", "vet", "fmt"},
	"rg": nil,
	// F-076: the build/test commands of every ecosystem DHI verifies.
	// They run project-defined code (npm scripts, uv run, build scripts,
	// Maven/Gradle goals) in a worktree the agent can edit, so this list
	// is a convenience, not a boundary — the OS sandbox is.
	"npm":      {"test", "run", "ci", "install"},
	"corepack": {"pnpm", "yarn"},
	"uv":       {"run", "sync", "pip", "venv"},
	"cargo":    {"build", "test", "check", "fmt", "clippy", "fetch"},
	"mvn":      {"test", "verify", "compile", "package", "-q", "-o", "-B"},
	"gradle":   {"test", "build", "check", "assemble", "--offline", "--quiet"},
	"dotnet":   {"build", "test", "restore", "format"},
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
				Description: "Run an allowlisted command: go build/test/vet/fmt, rg, npm test/run/ci/install, corepack pnpm/yarn, uv run/sync/pip/venv, cargo build/test/check/fmt/clippy/fetch, mvn, gradle, dotnet build/test/restore/format. No shell. Args: {\"program\":\"go\",\"args\":[\"test\",\"./...\"]}. Mutating: crosses approvals.",
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
				allowNet := d.networkAllowed()
				argv := dec.(runPlan).argv
				out, err := d.runWith(ctx, argv, allowNet)
				if d.OnRun != nil {
					d.OnRun(argv, err)
				}
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
		{
			info: mcp.ToolInfo{
				Name:        "skill_run",
				Description: "Run a script from one of your skills (shebang, no shell) under the sandbox. Args: {\"skill\": \"<name>\", \"script\": \"scripts/<file>\", \"args\": [\"...\"]}. Mutating: crosses approvals.",
				InputSchema: json.RawMessage(`{"type":"object","required":["skill","script"],"properties":{"skill":{"type":"string"},"script":{"type":"string"},"args":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a struct {
					Skill  string   `json:"skill"`
					Script string   `json:"script"`
					Args   []string `json:"args"`
				}
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				s, sc := strings.TrimSpace(a.Skill), strings.TrimSpace(a.Script)
				if s == "" || sc == "" {
					return nil, fmt.Errorf("skill and script are required")
				}
				return runPlan{argv: append([]string{s, sc}, a.Args...)}, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				plan := dec.(runPlan)
				if d.Skills == nil {
					return "", fmt.Errorf("skill library unavailable")
				}
				if d.Run == nil {
					return "", fmt.Errorf("command runner unavailable (run bootstrap)")
				}
				if strings.TrimSpace(d.Workdir) == "" {
					return "", fmt.Errorf("no working directory for this turn")
				}
				if !d.assigned(plan.argv[0]) {
					return "", fmt.Errorf("skill %q is not one of yours (ask the human to give it to you in Settings → SKILLS)", plan.argv[0])
				}
				script, err := d.Skills.Script(plan.argv[0], plan.argv[1])
				if err != nil {
					return "", err
				}
				allowNet := d.networkAllowed()
				argv := append([]string{script}, plan.argv[2:]...)
				out, err := d.runWith(ctx, argv, allowNet)
				if err != nil {
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
