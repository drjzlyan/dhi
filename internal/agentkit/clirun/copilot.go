// Copilot adapter (F-013 wave 3).
//
// Binary:   copilot  (@github/copilot HEADLESS CLI; npm -g)
// Invoked:  copilot -p <prompt> -s --no-ask-user --output-format=json
//
//	[--model M]
//
// Documented JSONL envelope (TYPE lines, session-store shaped; see the
// copilot CLI's session format + github/copilot-cli#52):
//
//	session.start            → ignore
//	user.message             → our prompt echo; ignore
//	assistant.message        → data.content (text), data.toolRequests
//	                           (array of { name, arguments }), tokens
//	tool.execution_start     → ignore (toolRequests already listed)
//	tool.execution_end       → data.name + data.result (error surfacing)
//	session.termination|shutdown → terminal; may carry usage
//
// NOTE — fixture-first (2026-09-09): copilot is NOT installed on the
// dev machine, so this adapter (parser + fixtures) is built to the
// documented envelope above. LIVE-VERIFY CHECKLIST (fill before this
// adapter's doctor row may report OK):
//  1. `copilot --version` banner + extracted version token.
//  2. Exact flag set for a quiet non-interactive run (`-p`, `-s`,
//     `--no-ask-user`, `--output-format=json`) — confirm -s vs the
//     usage stat blocks still emits JSONL.
//  3. Stream shapes: assistant.message content/toolRequests, tool
//     execution_end result, the termination event and where usage
//     (input/output tokens) actually appears (assistant.message or
//     termination).
//  4. Cost location: copilot reports no US-dollar cost → HasCost stays
//     false unless live runs place one.
//  5. Env keys confirmed: COPILOT_GITHUB_TOKEN (token auth).
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

// Copilot is the GitHub Copilot CLI adapter. Registered under the
// runtime value "copilot".
var Copilot = &CLI{
	Name:   "copilot",
	Bin:    "copilot",
	Tested: "1.0.88", // live-verified 2026-09-26
	// EXACT declared pass-through: the Copilot token + standard config
	// roots. Nothing else crosses (ADR-0012 §4).
	EnvPass: []string{
		"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
		"COPILOT_GITHUB_TOKEN",
	},
	StateRoot: func() []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return []string{filepath.Join(home, ".config", "github-copilot")}
	},
	BuildArgs: func(in RunInput) []string {
		// No verified system-prompt flag: the system block rides ahead
		// of the prompt in the shared tagged shape (M14 P1; live-verify
		// checklist: switch to a native flag if one lands). The prompt
		// stays the first positional arg.
		args := []string{
			"-p", PromptWithSystem(in.System, in.Prompt),
			"-s",
			"--no-ask-user",
			"--output-format=json",
			// Headless tool runs must not stall on an approval prompt;
			// the OS sandbox is the boundary (ADR-0012 §3).
			"--allow-all-tools",
		}
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		if in.MCPURL != "" {
			// Containment: drop copilot's built-in GitHub MCP server so
			// the agent's only non-native surface is DHI's endpoint.
			args = append(args, "--disable-builtin-mcps")
		}
		return args
	},
	ParseStream: copilotParseStream,
	Finalize:    copilotFinalize,
	Version:     copilotVersion,
	// MCPOK: copilot loads a workspace .mcp.json (project-local); the
	// runtime writes it into the worktree + git-excludes it;
	// live-verified 2026-09-26 on 1.0.88.
	MCPOK: true,
	MCPConfigFile: func(endpoint string) string {
		return fmt.Sprintf(`{"mcpServers":{"dhi":{"type":"http","url":%q}}}`, endpoint)
	},
	MCPProjectFile: ".mcp.json",
}

// copilotItem is the union shape of one JSONL envelope line.
// Live-verified 2026-09-26 against copilot 1.0.88: the terminal event is
// `result` (top-level exitCode); tools are `tool.execution_start` /
// `tool.execution_complete`; assistant text is `assistant.message`
// (data.content + data.toolRequests).
type copilotItem struct {
	Type     string `json:"type"`
	ExitCode int    `json:"exitCode"`
	Data     struct {
		Content      string `json:"content"`
		ToolRequests []struct {
			Name      string `json:"name"`
			Arguments struct {
				Command string `json:"command"`
			} `json:"arguments"`
		} `json:"toolRequests"`
		OutputTokens int    `json:"outputTokens"`
		InputTokens  int    `json:"inputTokens"`
		Name         string `json:"name"`
		ToolName     string `json:"toolName"`
		Success      bool   `json:"success"`
		Result       struct {
			Content string `json:"content"`
		} `json:"result"`
		Usage any `json:"usage"`
	} `json:"data"`
	Error any `json:"error"`
}

// copilotStop is the normalized terminal payload the parser hands
// Finalize: last assistant message + any reported tokens (copilot has
// no cost).
type copilotStop struct {
	Type        string `json:"type"` // "copilot.result"
	LastMessage string `json:"last_message"`
	ExitCode    int    `json:"exit_code"`
	Tokens      struct {
		Input  int `json:"input"`
		Output int `json:"output"`
	} `json:"tokens"`
}

func copilotParseStream(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		var lastText string
		for line := range lineStream(r) {
			var it copilotItem
			if err := json.Unmarshal([]byte(line), &it); err != nil {
				ch <- StreamEvent{Kind: EventError, Detail: truncate(err.Error(), 120)}
				continue
			}
			switch it.Type {
			case "result":
				stop := copilotStop{Type: "copilot.result", LastMessage: lastText, ExitCode: it.ExitCode}
				stop.Tokens.Input = it.Data.InputTokens
				stop.Tokens.Output = it.Data.OutputTokens
				b, err := json.Marshal(stop)
				if err != nil {
					ch <- StreamEvent{Kind: EventError, Detail: "copilot: marshal final"}
					continue
				}
				ch <- StreamEvent{Kind: EventFinal, Detail: string(b)}
			case "assistant.message":
				if t := strings.TrimSpace(it.Data.Content); t != "" {
					lastText = t
					ch <- StreamEvent{Kind: EventProgress, Detail: truncate(t, 200)}
				}
				for _, tr := range it.Data.ToolRequests {
					label := tr.Name
					if cmd := strings.TrimSpace(tr.Arguments.Command); cmd != "" {
						label += ": " + cmd
					}
					if label != "" {
						ch <- StreamEvent{Kind: EventCommand, Detail: "tool: " + truncate(label, 160)}
					}
				}
			case "tool.execution_complete":
				if !it.Data.Success {
					msg := it.Data.Name
					if msg == "" {
						msg = it.Data.ToolName
					}
					detail := strings.TrimSpace(it.Data.Result.Content)
					if detail == "" {
						detail = "failed"
					}
					ch <- StreamEvent{Kind: EventError, Detail: "tool " + msg + ": " + truncate(detail, 160)}
				}
			case "error":
				ch <- StreamEvent{Kind: EventError, Detail: truncate(fmt.Sprintf("%v", it.Error), 160)}
			}
		}
	}()
	return ch
}

// copilotFinalize parses the stitched terminal payload. Tokens surface
// when the session record reports them; copilot reports no US-dollar
// cost → HasCost stays false.
func copilotFinalize(final string) (string, Usage, error) {
	var stop copilotStop
	if err := json.Unmarshal([]byte(final), &stop); err != nil {
		return "", Usage{}, fmt.Errorf("clirun/copilot: final: %w", err)
	}
	if stop.ExitCode != 0 {
		return "", Usage{}, fmt.Errorf("clirun/copilot: run ended with exit %d", stop.ExitCode)
	}
	u := Usage{TokensIn: stop.Tokens.Input, TokensOut: stop.Tokens.Output}
	if stop.Tokens.Input == 0 {
		u.TokensIn = -1
	}
	if stop.Tokens.Output == 0 {
		u.TokensOut = -1
	}
	return strings.TrimSpace(stop.LastMessage), u, nil
}

// copilotVersion extracts the version token from `copilot --version`.
func copilotVersion(ctx context.Context, path string) (string, error) {
	out, err := probeVersion(ctx, path, "--version")
	if err != nil {
		return "", err
	}
	// "GitHub Copilot CLI 1.0.88." — drop the trailing period.
	for _, f := range strings.Fields(out) {
		f = strings.TrimPrefix(f, "v")
		f = strings.Trim(f, ".,")
		if isVersionLike(f) {
			return f, nil
		}
	}
	return "", fmt.Errorf("clirun/copilot: no version in %q", truncate(out, 60))
}
