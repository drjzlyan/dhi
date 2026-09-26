package clirun

import (
	"strings"
	"testing"
)

// TestSystemReachesEveryAdapter pins the M14 P1 contract: the system
// block reaches EVERY runtime — claude via --append-system-prompt, the
// rest via the shared tagged prompt shape. A missing system delivery is
// the bug this suite exists for (the gap that silently dropped persona
// + grounding + standards on five of six runtimes).
func TestSystemReachesEveryAdapter(t *testing.T) {
	for _, c := range allAdapters() {
		argv := strings.Join(c.BuildArgs(RunInput{
			Prompt: "do the thing", System: "PERSONA-MARKER",
		}), "\x00")
		if !strings.Contains(argv, "PERSONA-MARKER") {
			t.Errorf("%s: system block never reaches argv: %q", c.Name, argv)
		}
	}
}

// TestPromptWithSystemShape pins the tagged delivery shape.
func TestPromptWithSystemShape(t *testing.T) {
	if got := PromptWithSystem("", "just prompt"); got != "just prompt" {
		t.Fatalf("empty system = %q", got)
	}
	want := "<dhi-system>\npersona\n</dhi-system>\n\njust prompt"
	if got := PromptWithSystem("persona", "just prompt"); got != want {
		t.Fatalf("with system = %q, want %q", got, want)
	}
}

// TestStdinDeliveryAdapters pins which adapters own a verified stdin
// path: claude and codex yes (their contracts name it), the rest no —
// oversized prompts refuse by name on those, never E2BIG.
func TestStdinDeliveryAdapters(t *testing.T) {
	reg := map[string]bool{}
	for _, c := range allAdapters() {
		reg[c.Name] = c.StdinOK
	}
	if !reg["claude"] || !reg["codex"] {
		t.Fatalf("claude/codex must support stdin: %v", reg)
	}
	for _, n := range []string{"opencode", "antigravity", "copilot", "cursor"} {
		if reg[n] {
			t.Fatalf("%s claims stdin without a verified contract", n)
		}
	}
}

// TestClaudeStdinShape pins the oversized argv: `-p` with no prompt
// argument (claude reads stdin), no giant arg anywhere.
func TestClaudeStdinShape(t *testing.T) {
	argv := Claude.BuildArgs(RunInput{Stdin: "big blob"})
	joined := strings.Join(argv, "\x00")
	if strings.Contains(joined, "big blob") {
		t.Fatalf("stdin blob leaked into argv: %q", joined)
	}
	if argv[0] != "-p" {
		t.Fatalf("argv[0] = %q", argv[0])
	}
}

func TestMaxPromptArgBudget(t *testing.T) {
	if MaxPromptArg() < 64<<10 || MaxPromptArg() > 128<<10 {
		t.Fatalf("budget %d outside the sane band", MaxPromptArg())
	}
}

// TestMCPConfigWiring pins the F-028 adapter contract: only the
// MCP-capable adapter (claude) emits the config flags; adapters without
// verified wiring ignore MCPConfig entirely (their dhi-action fallback
// stays the contract).
func TestMCPConfigWiring(t *testing.T) {
	for _, c := range allAdapters() {
		argv := strings.Join(c.BuildArgs(RunInput{
			Prompt: "p", MCPConfig: "/tmp/dhi-mcp.json",
		}), "\x00")
		if c.Name == "claude" {
			if !strings.Contains(argv, "--mcp-config") || !strings.Contains(argv, "/tmp/dhi-mcp.json") {
				t.Errorf("claude argv missing --mcp-config: %q", argv)
			}
			if !strings.Contains(argv, "--strict-mcp-config") {
				t.Errorf("claude argv missing --strict-mcp-config: %q", argv)
			}
			continue
		}
		if strings.Contains(argv, "/tmp/dhi-mcp.json") {
			t.Errorf("%s leaked an unverified MCP config into argv: %q", c.Name, argv)
		}
	}
}

// TestMCPCapabilityDeclared pins which adapters claim verified MCP
// wiring: claude today; the rest stay fallback until their live-verify
// checklist is filled (ADR-0011/ADR-0017).
func TestMCPCapabilityDeclared(t *testing.T) {
	reg := map[string]bool{}
	for _, c := range allAdapters() {
		reg[c.Name] = c.MCPOK
	}
	if !reg["claude"] {
		t.Fatal("claude must declare MCP wiring")
	}
}
