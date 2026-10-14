package clirun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Claude is the Claude Code adapter, verified against 2.1.177
// (`claude --version` → "2.1.177 (Claude Code)"). Headless mode:
// `claude -p <prompt> --output-format stream-json --verbose` — a JSONL
// stream of system/assistant/user/result events whose terminal `result`
// line carries the final text, total cost, and token usage.
var Claude = &CLI{
	Name:    "claude",
	Bin:     "claude",
	Tested:  "2.1.177",
	EnvPass: []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL"},
	// Live-verified on 2.1.285: with only TMPDIR set, Bash still mkdirs
	// /private/tmp/claude-<uid>/<cwd> and fails EPERM under the sandbox;
	// CLAUDE_CODE_TMPDIR moves that base (F-086).
	TempEnv: []string{"CLAUDE_CODE_TMPDIR"},
	WaitEnv: []string{"MCP_TOOL_TIMEOUT"},
	StateRoot: func() []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return []string{filepath.Join(home, ".claude")}
	},
	BuildArgs: func(in RunInput) []string {
		args := []string{"-p"}
		if in.Stdin != "" {
			// Oversized delivery: `claude -p` with no prompt argument
			// reads the prompt from stdin; the system block rides
			// inside the streamed blob (PromptWithSystem shape).
			args = append(args,
				"--output-format", "stream-json",
				"--verbose",
				"--permission-mode", "bypassPermissions", // OS sandbox is the boundary (ADR-0012 §3)
				"--max-turns", claudeMaxTurns(in.MaxTurns),
			)
			return append(args, claudeResumeArgs(in.Resume)...)
		}
		args = append(args, in.Prompt,
			"--output-format", "stream-json",
			"--verbose",
			"--permission-mode", "bypassPermissions", // OS sandbox is the boundary (ADR-0012 §3)
			"--max-turns", claudeMaxTurns(in.MaxTurns),
		)
		args = append(args, claudeResumeArgs(in.Resume)...)
		if in.System != "" {
			args = append(args, "--append-system-prompt", in.System)
		}
		// IDE tools (F-028): the generated config registers DHI's
		// loopback MCP endpoint; strict keeps the user's own MCP
		// servers out of the agent run (nothing crosses).
		args = append(args, claudeMCPArgs(in.MCPConfig, in.MCPURL)...)
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		if in.Workdir != "" {
			args = append(args, "--add-dir", in.Workdir)
		}
		return args
	},
	ParseStream: claudeParseStream,
	Finalize:    claudeFinalize,
	Version:     claudeVersion,
	// StdinOK: `claude -p` with no prompt argument consumes stdin —
	// live-verified on 2.1.177.
	StdinOK: true,
	// ResumeOK: `-p … --resume <session_id>` continues the session the
	// result line's session_id names — live-verified on 2.1.285 (recall
	// across spawns, cache reads on the resumed turn).
	ResumeOK: true,
	// MCPOK: --mcp-config with an http-type server is a documented
	// Claude Code feature; live-verify checklist in the F-028 smoke.
	MCPOK:         true,
	MCPConfigFile: claudeMCPConfigFile,
	MCPConfigArgs: claudeMCPArgs,
}

// claudeMaxTurns renders --max-turns (0 = the adapter default, 50).
func claudeMaxTurns(n int) string {
	if n <= 0 {
		n = 50
	}
	return strconv.Itoa(n)
}

func claudeResumeArgs(id string) []string {
	if id == "" {
		return nil
	}
	return []string{"--resume", id}
}

func claudeMCPConfigFile(endpoint string) string {
	return fmt.Sprintf(`{"mcpServers":{"dhi":{"type":"http","url":%q}}}`, endpoint)
}

func claudeMCPArgs(configPath, _ string) []string {
	if configPath == "" {
		return nil
	}
	return []string{"--mcp-config", configPath, "--strict-mcp-config"}
}

func allAdapters() []*CLI {
	return []*CLI{Claude, Codex, OpenCode, CursorAgent, Copilot, Antigravity}
}

func claudeParseStream(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		for line := range lineStream(r) {
			ev, err := parseJSONLine(line)
			if err != nil {
				ch <- StreamEvent{Kind: EventError, Detail: truncate(err.Error(), 120)}
				continue
			}
			for _, e := range claudeLineEvents(ev) {
				ch <- e
			}
		}
	}()
	return ch
}

// claudeLineEvents maps one decoded line to neutral events.
func claudeLineEvents(ev map[string]any) []StreamEvent {
	typ, _ := ev["type"].(string)
	switch typ {
	case "assistant":
		return claudeAssistantEvents(ev)
	case "user":
		return claudeUserEvents(ev)
	case "result":
		return []StreamEvent{{Kind: EventFinal, Detail: reencode(ev)}}
	case "system":
		if sub, _ := ev["subtype"].(string); sub == "init" {
			if id, _ := ev["session_id"].(string); id != "" {
				return []StreamEvent{{Kind: EventSession, Session: id}}
			}
		}
		return nil
	default:
		return nil // system/init and the like carry nothing to show
	}
}

func claudeAssistantEvents(ev map[string]any) []StreamEvent {
	msg, _ := ev["message"].(map[string]any)
	content, _ := msg["content"].([]any)
	var out []StreamEvent
	if u, ok := msg["usage"].(map[string]any); ok {
		n := func(k string) int { f, _ := u[k].(float64); return int(f) }
		if ctx := n("input_tokens") + n("cache_read_input_tokens") + n("cache_creation_input_tokens"); ctx > 0 {
			out = append(out, StreamEvent{Kind: EventUsage, Context: ctx})
		}
	}
	for _, raw := range content {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch b["type"].(string) {
		case "text":
			if t, _ := b["text"].(string); strings.TrimSpace(t) != "" {
				out = append(out, StreamEvent{Kind: EventProgress, Detail: truncate(strings.TrimSpace(t), 200)})
			}
		case "tool_use":
			name, _ := b["name"].(string)
			out = append(out, StreamEvent{Kind: EventCommand, Detail: claudeToolDetail(name, b["input"])})
		}
	}
	return out
}

func claudeUserEvents(ev map[string]any) []StreamEvent {
	msg, _ := ev["message"].(map[string]any)
	content, _ := msg["content"].([]any)
	var out []StreamEvent
	for _, raw := range content {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if b["type"].(string) != "tool_result" {
			continue
		}
		if isErr, _ := b["is_error"].(bool); isErr {
			text := toolResultText(b["content"])
			out = append(out, StreamEvent{Kind: EventError, Detail: truncate(text, 200), Denied: DeniedPath(text)})
		}
	}
	return out
}

// claudeToolDetail renders a tool_use as one transcript line.
func claudeToolDetail(name string, input any) string {
	m, _ := input.(map[string]any)
	switch name {
	case "Bash":
		if cmd, _ := m["command"].(string); cmd != "" {
			return "bash: " + truncate(strings.TrimSpace(cmd), 160)
		}
	case "Write", "Edit", "NotebookEdit":
		if p, _ := m["file_path"].(string); p != "" {
			return name + ": " + p
		}
	case "Read", "Glob", "Grep":
		if p, _ := m["path"].(string); p != "" {
			return name + ": " + p
		}
	}
	return name
}

func toolResultText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, raw := range v {
			if b, ok := raw.(map[string]any); ok {
				if t, _ := b["text"].(string); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// claudeFinalize parses the terminal result line.
func claudeFinalize(final string) (string, Usage, error) {
	var res struct {
		Type      string  `json:"type"`
		Subtype   string  `json:"subtype"`
		IsError   bool    `json:"is_error"`
		Result    string  `json:"result"`
		TotalCost float64 `json:"total_cost_usd"`
		SessionID string  `json:"session_id"`
		Usage     struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			CacheRead    int `json:"cache_read_input_tokens"`
			CacheWrite   int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(final), &res); err != nil {
		return "", Usage{}, fmt.Errorf("clirun/claude: final: %w", err)
	}
	if res.IsError || strings.HasPrefix(res.Subtype, "error") {
		// The result text carries the reason ("Prompt is too long",
		// "Invalid API key · Please run /login"); keep it so the
		// runtime can classify the failure (F-065).
		if why := strings.TrimSpace(res.Result); why != "" {
			return "", Usage{}, fmt.Errorf("clirun/claude: run ended with %q: %s", res.Subtype, truncate(why, 300))
		}
		return "", Usage{}, fmt.Errorf("clirun/claude: run ended with %q", res.Subtype)
	}
	u := Usage{
		TokensIn:  res.Usage.InputTokens,
		TokensOut: res.Usage.OutputTokens,
		CostUSD:   res.TotalCost,
		HasCost:   res.TotalCost > 0 || res.Usage.InputTokens > 0,

		CacheRead:  res.Usage.CacheRead,
		CacheWrite: res.Usage.CacheWrite,
		SessionID:  res.SessionID,
	}
	return strings.TrimSpace(res.Result), u, nil
}

// claudeVersion extracts the version token from `claude --version`
// ("2.1.177 (Claude Code)").
func claudeVersion(ctx context.Context, path string) (string, error) {
	out, err := probeVersion(ctx, path)
	if err != nil {
		return "", err
	}
	for _, f := range strings.Fields(out) {
		if isVersionLike(f) {
			return f, nil
		}
	}
	return "", fmt.Errorf("clirun/claude: no version in %q", truncate(out, 60))
}

func isVersionLike(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	dots := strings.Count(s, ".")
	if dots < 1 || dots > 2 {
		return false
	}
	for _, r := range s {
		if r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func reencode(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
