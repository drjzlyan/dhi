package clirun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/mcp"
)

// probeHandler is a one-tool MCP server used to prove an adapter can
// actually reach DHI's loopback endpoint end-to-end.
type probeHandler struct{ called chan string }

func (p *probeHandler) ProtocolVersion() string      { return mcp.ProtocolVersion }
func (p *probeHandler) ServerInfo() (string, string) { return "dhi-live-probe", "0" }
func (p *probeHandler) Tools() []mcp.ToolInfo {
	return []mcp.ToolInfo{{
		Name:        "echo_probe",
		Description: "Echo the given text back. Arguments: {\"text\": string}.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
	}}
}
func (p *probeHandler) CallTool(_ context.Context, name string, args json.RawMessage) (string, bool, error) {
	if name != "echo_probe" {
		return "", true, fmt.Errorf("unknown tool %q", name)
	}
	select {
	case p.called <- string(args):
	default:
	}
	return "PROBE_OK", false, nil
}

// TestLiveOpenCodeMCP is the live-verify for the opencode MCP wiring:
// it starts DHI's real loopback server, points OPENCODE_CONFIG at a
// generated config, runs `opencode run`, and asserts the agent actually
// called the served tool. Gated: real run, real tokens.
//
//	DHI_LIVE_MCP=1 go test ./internal/agentkit/clirun/ -run TestLiveOpenCodeMCP -v
func TestLiveOpenCodeMCP(t *testing.T) {
	if os.Getenv("DHI_LIVE_MCP") == "" {
		t.Skip("set DHI_LIVE_MCP=1 to run the real opencode MCP verification (costs tokens)")
	}
	path, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not installed")
	}

	h := &probeHandler{called: make(chan string, 1)}
	endpoint, stop, err := mcp.ServeLoopback(h)
	if err != nil {
		t.Fatalf("ServeLoopback: %v", err)
	}
	defer stop()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(cfgPath, []byte(OpenCode.MCPConfigFile(endpoint)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	work := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	argv := OpenCode.BuildArgs(RunInput{
		Prompt:  "Call the MCP tool named echo_probe with text=\"HELLO\", then reply with its output.",
		Workdir: work,
	})
	cmd := exec.CommandContext(ctx, path, argv...)
	cmd.Dir = work
	cmd.Env = withOverride(os.Environ(), OpenCode.MCPConfigEnv, cfgPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opencode run: %v\n%s", err, out)
	}
	select {
	case got := <-h.called:
		t.Logf("LIVE-VERIFIED: opencode called DHI's served tool with args %s", got)
		if !strings.Contains(got, "HELLO") {
			t.Fatalf("tool called with unexpected args: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("opencode did not call the served tool; output:\n%s", out)
	}
}

// withOverride returns env with key set to val, dropping any prior value
// (a duplicate would otherwise let the user's value win).
func withOverride(env []string, key, val string) []string {
	out := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return append(out, key+"="+val)
}

// TestLiveClaudeMCP is the live-verify for the claude MCP wiring:
// --mcp-config + --strict-mcp-config must register DHI's loopback server
// and the agent must actually call the served tool.
//
//	DHI_LIVE_MCP=1 go test ./internal/agentkit/clirun/ -run TestLiveClaudeMCP -v
func TestLiveClaudeMCP(t *testing.T) {
	if os.Getenv("DHI_LIVE_MCP") == "" {
		t.Skip("set DHI_LIVE_MCP=1 to run the real claude MCP verification (costs tokens)")
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not installed")
	}
	h := &probeHandler{called: make(chan string, 1)}
	endpoint, stop, err := mcp.ServeLoopback(h)
	if err != nil {
		t.Fatalf("ServeLoopback: %v", err)
	}
	defer stop()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "claude-mcp.json")
	if err := os.WriteFile(cfgPath, []byte(Claude.MCPConfigFile(endpoint)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	argv := Claude.BuildArgs(RunInput{
		Prompt:    "Call the MCP tool named echo_probe (server dhi) with text=\"HELLO\", then reply with its output.",
		MCPConfig: cfgPath, Workdir: work,
	})
	cmd := exec.CommandContext(ctx, path, argv...)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("claude run: %v\n%s", err, out)
	}
	select {
	case got := <-h.called:
		t.Logf("LIVE-VERIFIED: claude called DHI's served tool with args %s", got)
		if !strings.Contains(got, "HELLO") {
			t.Fatalf("tool called with unexpected args: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("claude did not call the served tool; output:\n%s", out)
	}
}

// TestLiveCodexMCP is the live-verify for the codex MCP wiring:
// `-c mcp_servers.dhi.url=...` must register DHI's loopback server and
// the agent must actually call the served tool.
//
//	DHI_LIVE_MCP=1 go test ./internal/agentkit/clirun/ -run TestLiveCodexMCP -v
func TestLiveCodexMCP(t *testing.T) {
	if os.Getenv("DHI_LIVE_MCP") == "" {
		t.Skip("set DHI_LIVE_MCP=1 to run the real codex MCP verification (costs tokens)")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex not installed")
	}
	h := &probeHandler{called: make(chan string, 1)}
	endpoint, stop, err := mcp.ServeLoopback(h)
	if err != nil {
		t.Fatalf("ServeLoopback: %v", err)
	}
	defer stop()
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	argv := Codex.BuildArgs(RunInput{
		Prompt: "Call the MCP tool named echo_probe with text=\"HELLO\" (server dhi), then reply with its output.",
		MCPURL: endpoint, Workdir: work,
	})
	cmd := exec.CommandContext(ctx, path, argv...)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codex run: %v\n%s", err, out)
	}
	select {
	case got := <-h.called:
		t.Logf("LIVE-VERIFIED: codex called DHI's served tool with args %s", got)
		if !strings.Contains(got, "HELLO") {
			t.Fatalf("tool called with unexpected args: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("codex did not call the served tool; output:\n%s", out)
	}
}
