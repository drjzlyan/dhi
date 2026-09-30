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
// never leak the path into argv.
func TestMCPConfigWiring(t *testing.T) {
	const path = "/tmp/dhi-mcp.json"
	const url = "http://127.0.0.1:1/mcp"
	const geminiDir = "/tmp/dhi-gemini"
	for _, c := range allAdapters() {
		in := RunInput{Prompt: "p", MCPConfig: path, MCPURL: url, MCPGeminiDir: geminiDir}
		argv := strings.Join(c.BuildArgs(in), "\x00")
		if !c.MCPOK {
			if c.MCPConfigFile != nil || c.MCPConfigEnv != "" || c.MCPConfigArgs != nil || c.MCPGeminiDir != nil {
				t.Errorf("%s declares MCP wiring without MCPOK", c.Name)
			}
			if strings.Contains(argv, path) || strings.Contains(argv, url) || strings.Contains(argv, geminiDir) {
				t.Errorf("%s leaked an unverified MCP wiring into argv: %q", c.Name, argv)
			}
			continue
		}
		// Complete injection: at least one delivery path.
		if !c.MCPWired() {
			t.Errorf("%s: MCPOK without any wiring", c.Name)
		}
		switch {
		case c.MCPGeminiDir != nil:
			if c.MCPGeminiDirFlag == "" || !strings.Contains(argv, geminiDir) {
				t.Errorf("%s: dir-delivered adapter must emit %s=<dir>: %q", c.Name, c.MCPGeminiDirFlag, argv)
			}
		case c.MCPConfigArgs != nil:
			if !strings.Contains(argv, path) && !strings.Contains(argv, url) {
				t.Errorf("%s argv missing MCP wiring (path or url): %q", c.Name, argv)
			}
		default:
			if strings.Contains(argv, path) || strings.Contains(argv, url) {
				t.Errorf("%s is env-delivered but leaked wiring into argv: %q", c.Name, argv)
			}
		}
	}
}

// TestMCPCapabilityDeclared pins which adapters claim verified MCP
// wiring: claude (argv) + opencode (env) + codex (argv) + cursor
// (project file) + copilot (project file) + antigravity (--gemini_dir).
func TestMCPCapabilityDeclared(t *testing.T) {
	reg := map[string]bool{}
	for _, c := range allAdapters() {
		reg[c.Name] = c.MCPOK
	}
	for _, n := range []string{"claude", "opencode", "codex", "cursor-agent", "copilot", "antigravity"} {
		if !reg[n] {
			t.Fatalf("%s must declare MCP wiring: %v", n, reg)
		}
	}
}

// TestCodexMCPArgs pins the inline `-c` override shape (TOML string).
func TestCodexMCPArgs(t *testing.T) {
	got := Codex.MCPArgs("", "http://127.0.0.1:5/mcp")
	want := []string{"-c", `mcp_servers.dhi.url="http://127.0.0.1:5/mcp"`}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("codex MCP args = %q, want %q", got, want)
	}
	if Codex.MCPArgs("", "") != nil {
		t.Fatal("empty endpoint must yield nil args")
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
