package clirun

import (
	"encoding/json"
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

// TestMCPConfigWiring pins the F-028 adapter contract: every MCPOK
// adapter declares a complete injection (a config-file renderer plus
// either argv or env); argv takers emit the flags; env takers never
// leak the path into argv (their dhi-action fallback stays otherwise).
func TestMCPConfigWiring(t *testing.T) {
	const path = "/tmp/dhi-mcp.json"
	for _, c := range allAdapters() {
		argv := strings.Join(c.BuildArgs(RunInput{Prompt: "p", MCPConfig: path}), "\x00")
		if !c.MCPOK {
			if c.MCPConfigFile != nil || c.MCPConfigEnv != "" || c.MCPConfigArgs != nil {
				t.Errorf("%s declares MCP wiring without MCPOK", c.Name)
			}
			if strings.Contains(argv, path) {
				t.Errorf("%s leaked an unverified MCP config into argv: %q", c.Name, argv)
			}
			continue
		}
		// Complete injection: a renderer, and a delivery path.
		if c.MCPConfigFile == nil {
			t.Errorf("%s: MCPOK without MCPConfigFile", c.Name)
		}
		if c.MCPConfigEnv == "" && c.MCPConfigArgs == nil {
			t.Errorf("%s: MCPOK with no delivery (env or args)", c.Name)
		}
		if c.MCPConfigArgs != nil {
			if !strings.Contains(argv, path) {
				t.Errorf("%s argv missing the MCP config path: %q", c.Name, argv)
			}
		} else if strings.Contains(argv, path) {
			t.Errorf("%s is env-delivered but leaked the path into argv: %q", c.Name, argv)
		}
	}
}

// TestMCPCapabilityDeclared pins which adapters claim verified MCP
// wiring: claude (argv) + opencode (env) today; the rest stay fallback
// until their live-verify checklist is filled (ADR-0011/ADR-0017).
func TestMCPCapabilityDeclared(t *testing.T) {
	reg := map[string]bool{}
	for _, c := range allAdapters() {
		reg[c.Name] = c.MCPOK
	}
	if !reg["claude"] || !reg["opencode"] {
		t.Fatalf("claude+opencode must declare MCP wiring: %v", reg)
	}
}

// TestMCPEnvAndFileRenderers pins the opencode delivery: MCPEnv points
// OPENCODE_CONFIG at the per-turn file, and the renderer emits a remote
// `dhi` server carrying the loopback endpoint.
func TestMCPEnvAndFileRenderers(t *testing.T) {
	if got := OpenCode.MCPEnv("/tmp/cfg.json"); len(got) != 1 || got[0] != "OPENCODE_CONFIG=/tmp/cfg.json" {
		t.Fatalf("OpenCode.MCPEnv = %v", got)
	}
	if got := OpenCode.MCPEnv(""); got != nil {
		t.Fatalf("empty config must yield nil env, got %v", got)
	}
	body := OpenCode.MCPConfigFile("http://127.0.0.1:1234/mcp")
	var cfg struct {
		MCP map[string]struct {
			Type    string `json:"type"`
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("opencode config not JSON: %v (%s)", err, body)
	}
	d := cfg.MCP["dhi"]
	if d.Type != "remote" || d.URL != "http://127.0.0.1:1234/mcp" || !d.Enabled {
		t.Fatalf("opencode dhi server = %+v", d)
	}
	if !strings.Contains(Claude.MCPConfigFile("http://x/mcp"), `"mcpServers"`) {
		t.Fatal("claude renderer must emit mcpServers")
	}
}
