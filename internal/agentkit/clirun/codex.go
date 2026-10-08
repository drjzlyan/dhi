// Codex adapter (F-013 wave 2).
//
// Binary:   codex  (codex-cli; tested vs 0.147.0)
// Invoked:  codex exec --json --skip-git-repo-check -C <workdir>
//
//	[-m <model>] --sandbox danger-full-access
//	--dangerously-bypass-approvals-and-sandbox "<prompt>"
//
// The exec JSONL stream (stdout) is the contract:
//
//	thread.started            → ignore
//	turn.started              → ignore
//	item.started              → agent_message | command_execution begins
//	item.completed            → agent_message { text }
//	                           | command_execution { command, status,
//	                             aggregated_output, exit_code }
//	                           | file_change { path }
//	turn.completed            → usage { input_tokens, cached_input_tokens,
//	                             cache_write_input_tokens, output_tokens,
//	                             reasoning_output_tokens }
//
// codex reports no US-dollar cost → HasCost false. Token counts come from
// turn.completed. Stderr may carry noise (models-cache errors) — only
// stdout is parsed. The F-013 "danger-full-access" bypass pair is
// deliberate: codex's tiny built-in sandbox must not double-wrap or stall
// on approvals; DHI's OS sandbox from the Guard seam is the boundary.
//
// Live-verified 2026-09-09: codex-cli 0.147.0; full event vocabulary above
// observed: turn.completed carries the usage block; other item kinds
// appear on some CLIs and are tolerated as noise.
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

// Codex is the Codex adapter. Registered under the runtime value "codex".
var Codex = &CLI{
	Name:   "codex",
	Bin:    "codex",
	Tested: "0.147.0",
	// EXACT declared pass-through: codex's own config dir plus the
	// provider keys it reads for hosted models. Nothing else crosses
	// (ADR-0012 §4).
	EnvPass: []string{
		"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
		"CODEX_HOME",
		"OPENAI_API_KEY", "OPENAI_BASE_URL",
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL",
	},
	StateRoot: func() []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return []string{filepath.Join(home, ".codex")}
	},
	BuildArgs: func(in RunInput) []string {
		args := []string{
			"exec", "--json", "--skip-git-repo-check",
			"-C", in.Workdir,
		}
		if in.Model != "" {
			args = append(args, "-m", in.Model)
		}
		args = append(args,
			// OS sandbox is the boundary; codex's own sandbox must not
			// double-wrap or pause on interactive approval prompts.
			"--sandbox", "danger-full-access",
			"--dangerously-bypass-approvals-and-sandbox",
		)
		// IDE tools (F-028): register DHI's loopback endpoint inline via
		// `-c mcp_servers.dhi.url=...` (no config file needed).
		args = append(args, CodexMCPArgs(in.MCPConfig, in.MCPURL)...)
		if in.Stdin != "" {
			// Oversized delivery: `codex exec -` reads the prompt from
			// stdin (0.147 contract; live-verify checklist item).
			args = append(args, "-")
			return args
		}
		// No verified system-prompt flag in exec mode: the system block
		// rides ahead of the prompt in the shared tagged shape (M14 P1;
		// live-verify checklist: switch to a native flag if one lands).
		// The prompt stays the final positional arg.
		args = append(args, PromptWithSystem(in.System, in.Prompt))
		return args
	},
	ParseStream: codexParseStream,
	Finalize:    codexFinalize,
	Version:     codexVersion,
	// StdinOK: `codex exec -` consumes stdin (documented contract;
	// live-verify on the next real run).
	StdinOK: true,
	// MCPOK: `-c mcp_servers.dhi.url=...` registers the loopback
	// endpoint inline; live-verified 2026-09-26 on 0.147.0 (a real
	// tool call through the loopback server).
	MCPOK:         true,
	MCPConfigArgs: CodexMCPArgs,
}

// CodexMCPArgs builds the inline `-c` override registering DHI's
// endpoint (codex parses the value as TOML, hence the quoted string).
func CodexMCPArgs(_, endpoint string) []string {
	if endpoint == "" {
		return nil
	}
	return []string{"-c", fmt.Sprintf("mcp_servers.dhi.url=%q", endpoint)}
}

// codexItem is the union shape of one exec JSONL line.
type codexItem struct {
	Type string `json:"type"`
	Item struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Command  string `json:"command"`
		Status   string `json:"status"`
		ExitCode int    `json:"exit_code"`
		Output   string `json:"aggregated_output"`
		Path     string `json:"path"`
	} `json:"item"`
	Path  string `json:"path"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// codexStop is the normalized terminal payload the parser hands Finalize:
// codex never puts the final assistant text and the usage block on one
// line, so the parser stitches them together (raw turn.completed fields
// preserved; last_message added).
type codexStop struct {
	Type        string `json:"type"` // "codex.turn.completed"
	LastMessage string `json:"last_message"`
	Usage       struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func codexParseStream(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		var lastText string
		for line := range lineStream(r) {
			var it codexItem
			if err := json.Unmarshal([]byte(line), &it); err != nil {
				ch <- StreamEvent{Kind: EventError, Detail: truncate(err.Error(), 120)}
				continue
			}
			if it.Type == "turn.completed" {
				stop := codexStop{Type: "codex.turn.completed", LastMessage: lastText}
				stop.Usage.InputTokens = it.Usage.InputTokens
				stop.Usage.OutputTokens = it.Usage.OutputTokens
				b, err := json.Marshal(stop)
				if err != nil {
					ch <- StreamEvent{Kind: EventError, Detail: "codex: marshal final"}
					continue
				}
				ch <- StreamEvent{Kind: EventFinal, Detail: string(b)}
				continue
			}
			if ev, ok := codexLineEvents(it); ok {
				ch <- ev
			}
			if it.Type == "item.completed" && it.Item.Type == "agent_message" && strings.TrimSpace(it.Item.Text) != "" {
				lastText = it.Item.Text
			}
		}
	}()
	return ch
}

// codexLineEvents maps one decoded line to a neutral event.
func codexLineEvents(it codexItem) (StreamEvent, bool) {
	switch it.Type {
	case "item.started", "item.completed":
		switch it.Item.Type {
		case "command_execution":
			// The started event shows the command line; the completed
			// event carries exit/status so we only surface failures
			// there (no duplicate transcript rows for OK commands).
			if it.Type == "item.completed" {
				if it.Item.Status == "completed" && it.Item.ExitCode != 0 {
					return StreamEvent{Kind: EventError, Detail: fmt.Sprintf(
						"command failed (exit %d): %s", it.Item.ExitCode,
						truncate(strings.TrimSpace(it.Item.Output), 120),
					)}, true
				}
				return StreamEvent{}, false
			}
			return StreamEvent{Kind: EventCommand, Detail: "command: " +
				truncate(strings.TrimSpace(it.Item.Command), 160)}, true
		case "file_change":
			return StreamEvent{Kind: EventCommand, Detail: "file_change: " + it.Item.Path}, true
		case "agent_message":
			text := strings.TrimSpace(it.Item.Text)
			if text == "" {
				return StreamEvent{}, false
			}
			return StreamEvent{Kind: EventProgress, Detail: truncate(text, 200)}, true
		default:
			// util_notes and other item kinds are structural noise.
			return StreamEvent{}, false
		}
	default:
		// thread.started, turn.started, everything else.
		return StreamEvent{}, false
	}
}

// codexFinalize parses the normalized terminal payload for token usage.
// codex reports no cost → HasCost stays false (CostUSD 0).
func codexFinalize(final string) (string, Usage, error) {
	var stop codexStop
	if err := json.Unmarshal([]byte(final), &stop); err != nil {
		return "", Usage{}, fmt.Errorf("clirun/codex: final: %w", err)
	}
	u := Usage{TokensIn: stop.Usage.InputTokens, TokensOut: stop.Usage.OutputTokens}
	if u.TokensIn == 0 {
		u.TokensIn = -1
	}
	if u.TokensOut == 0 {
		u.TokensOut = -1
	}
	return strings.TrimSpace(stop.LastMessage), u, nil
}

// codexVersion extracts the version token from `codex --version`
// ("codex-cli 0.147.0").
func codexVersion(ctx context.Context, path string) (string, error) {
	out, err := probeVersion(ctx, path)
	if err != nil {
		return "", err
	}
	if v := firstVersionToken(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("clirun/codex: no version in %q", truncate(out, 60))
}
