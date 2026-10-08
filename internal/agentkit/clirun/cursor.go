// CursorAgent adapter (F-013 wave 3).
//
// Binary:   cursor-agent  (headless mode of the Cursor editor agent;
//
//	also invocable as `agent`)
//
// Invoked:  cursor-agent -p <prompt> --output-format stream-json
//
//	--force --trust [--model M] --workspace <workdir>
//
// Documented stream-json contract (cursor.com/docs/cli/headless):
//
//	system       → session metadata (model); no transcript text
//	assistant    → the agent's message text and tool calls
//	tool_call    → { type "started"|"completed", tool, title, error }
//	result       → terminal event; carries at minimum a duration
//
// NOTE — fixture-first (2026-09-09): cursor-agent is NOT installed on
// the dev machine, so this adapter (parser + fixtures) is built to the
// documented contract above. LIVE-VERIFY CHECKLIST (fill before this
// adapter's doctor row may report OK):
//  1. `cursor-agent --version` banner + extracted version token.
//  2. Exact headless flag set (`-p`, `--output-format stream-json`,
//     `--force`, `--trust`, `--model`, `--workspace`) on a real run.
//  3. Stream shapes: assistant text field, tool_call started/completed
//     subtleties, the terminal result event and whether it carries
//     text/tokens (parser currently stitches last_message).
//  4. Cost/token location: where (if anywhere) usage appears; thread
//     it through Finalize and set HasCost accordingly.
//  5. Env keys confirmed: CURSOR_API_KEY (or config-file auth).
package clirun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CursorAgent is the Cursor agent adapter. Registered under the runtime
// value "cursor-agent".
var CursorAgent = &CLI{
	Name:   "cursor-agent",
	Bin:    "cursor-agent",
	Tested: "2026.09.26", // live-verified 2026-09-26
	// EXACT declared pass-through: the Cursor API key + standard config
	// roots. Nothing else crosses (ADR-0012 §4).
	EnvPass: []string{
		"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
		"CURSOR_API_KEY",
	},
	StateRoot: func() []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return []string{filepath.Join(home, ".cursor")}
	},
	BuildArgs: func(in RunInput) []string {
		// No verified system-prompt flag: the system block rides ahead
		// of the prompt in the shared tagged shape (M14 P1; live-verify
		// checklist: switch to a native flag if one lands). The prompt
		// stays the first positional arg.
		args := []string{
			"-p", PromptWithSystem(in.System, in.Prompt),
			"--output-format", "stream-json",
			"--force",
			"--trust",
		}
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		if in.Workdir != "" {
			args = append(args, "--workspace", in.Workdir)
		}
		if in.MCPURL != "" {
			// Auto-approve the served MCP server without a prompt.
			args = append(args, "--approve-mcps")
		}
		return args
	},
	ParseStream: cursorParseStream,
	Finalize:    cursorFinalize,
	Version:     cursorVersion,
	// MCPOK: cursor-agent reads a project-local .cursor/mcp.json (no
	// argv/env override), so the runtime writes it into the worktree and
	// git-excludes it; live-verified 2026-09-26 on 2026.09.26.
	MCPOK: true,
	MCPConfigFile: func(endpoint string) string {
		return fmt.Sprintf(`{"mcpServers":{"dhi":{"url":%q}}}`, endpoint)
	},
	MCPProjectFile: ".cursor/mcp.json",
}

// cursorItem is the union shape of one stream-json line. Live-verified
// 2026-09-26 against cursor-agent 2026.09.26: assistant text is nested
// under message.content[], `result` carries the final text + camelCase
// usage, and thinking deltas ride their own subtype (ignored).
type cursorItem struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Text    string `json:"text"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Tool  string `json:"tool"`
	Title string `json:"title"`
	Error string `json:"error"`
	Usage struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"usage"`
}

// cursorStop is the normalized terminal payload the parser hands
// Finalize: the result event carries the final text and usage; the
// parser stitches the last assistant message when it does not.
type cursorStop struct {
	Type        string `json:"type"` // "cursor.result"
	LastMessage string `json:"last_message"`
	IsError     bool   `json:"is_error"`
	TokensIn    int    `json:"tokens_in"`
	TokensOut   int    `json:"tokens_out"`
}

func cursorParseStream(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		var lastText string
		for line := range lineStream(r) {
			var it cursorItem
			if err := json.Unmarshal([]byte(line), &it); err != nil {
				ch <- StreamEvent{Kind: EventError, Detail: truncate(err.Error(), 120)}
				continue
			}
			switch it.Type {
			case "result":
				msg := strings.TrimSpace(it.Result)
				if msg == "" {
					msg = lastText
				}
				stop := cursorStop{
					Type: "cursor.result", LastMessage: msg, IsError: it.IsError,
					TokensIn: it.Usage.InputTokens, TokensOut: it.Usage.OutputTokens,
				}
				b, err := json.Marshal(stop)
				if err != nil {
					ch <- StreamEvent{Kind: EventError, Detail: "cursor-agent: marshal final"}
					continue
				}
				ch <- StreamEvent{Kind: EventFinal, Detail: string(b)}
			case "assistant":
				if it.Message.Role == "user" {
					continue
				}
				var sb strings.Builder
				for _, part := range it.Message.Content {
					if part.Type == "text" {
						sb.WriteString(part.Text)
					}
				}
				text := strings.TrimSpace(sb.String())
				if text != "" {
					lastText = text
					ch <- StreamEvent{Kind: EventProgress, Detail: truncate(text, 200)}
				}
			case "tool_call":
				title := it.Title
				if title == "" {
					title = it.Tool
				}
				if it.Error != "" {
					ch <- StreamEvent{Kind: EventError, Detail: "tool failed: " + truncate(it.Error, 160)}
					continue
				}
				if it.Subtype == "started" {
					ch <- StreamEvent{Kind: EventCommand, Detail: "tool: " + truncate(title, 160)}
				}
			case "error":
				ch <- StreamEvent{Kind: EventError, Detail: truncate(it.Error, 160)}
			}
		}
	}()
	return ch
}

// cursorFinalize parses the terminal payload: a non-success result fails
// the run; tokens come from the result usage; cost is absent (HasCost
// false — cursor reports none).
func cursorFinalize(final string) (string, Usage, error) {
	var stop cursorStop
	if err := json.Unmarshal([]byte(final), &stop); err != nil {
		return "", Usage{}, fmt.Errorf("clirun/cursor-agent: final: %w", err)
	}
	if stop.IsError {
		return "", Usage{}, fmt.Errorf("clirun/cursor-agent: run ended with an error")
	}
	u := Usage{TokensIn: stop.TokensIn, TokensOut: stop.TokensOut}
	if stop.TokensIn == 0 {
		u.TokensIn = -1
	}
	if stop.TokensOut == 0 {
		u.TokensOut = -1
	}
	return strings.TrimSpace(stop.LastMessage), u, nil
}

// cursorVersion extracts the version token from `cursor-agent --version`.
func cursorVersion(ctx context.Context, path string) (string, error) {
	out, err := probeVersion(ctx, path)
	if err != nil {
		return "", err
	}
	// cursor reports "2026.09.26-dd393fe" (date + build hash): keep the
	// numeric date token, drop the hash.
	for _, f := range strings.Fields(out) {
		f = strings.TrimPrefix(f, "v")
		f = strings.TrimSuffix(f, ",")
		if i := strings.IndexByte(f, '-'); i > 0 {
			f = f[:i]
		}
		if isVersionLike(f) {
			return f, nil
		}
	}
	return "", fmt.Errorf("clirun/cursor-agent: no version in %q", truncate(out, 60))
}
