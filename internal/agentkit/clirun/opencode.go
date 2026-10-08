// OpenCode adapter (F-013 wave 2).
//
// Binary:   opencode  (tested vs 1.18.25)
// Invoked:  opencode run --format json --dir <workdir> --auto
//
//	[--model <model>] [--title <task-slug>] "<prompt>"
//
// The run JSONL stream (stdout) is the contract:
//
//	step_start   → a model step begins
//	tool_use     → part: { type "tool", tool: "bash"|…,
//	               state: { status, input.command, title, metadata.exit } }
//	text         → part: { type "text", text }
//	step_finish  → part: { reason: "stop"|"tool-calls",
//	               tokens: { total, input, output, reasoning,
//	                         cache: { write, read } }, cost }
//
// The final summary is the last `text` event before the stop step_finish;
// the parser stitches tokens+cost and last_message onto the terminal
// step_finish for Finalize. Cost is reported when the provider emits it
// (0 when unconfigured) → HasCost mirrors a non-zero cost.
//
// Live-verified 2026-09-09: opencode 1.18.25; full event vocabulary
// observed on a real run (bash tool + text + step_finish with tokens).
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

// OpenCode is the OpenCode adapter. Registered under the runtime value
// "opencode".
var OpenCode = &CLI{
	Name:   "opencode",
	Bin:    "opencode",
	Tested: "1.18.25",
	// EXACT declared pass-through: opencode's config + the provider
	// keys it reads for hosted models. Nothing else crosses
	// (ADR-0012 §4).
	EnvPass: []string{
		"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
		"OPENCODE_CONFIG", "OPENCODE_SERVER_PASSWORD",
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY",
	},
	StateRoot: func() []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return []string{filepath.Join(home, ".local", "share", "opencode")}
	},
	BuildArgs: func(in RunInput) []string {
		args := []string{"run", "--format", "json", "--dir", in.Workdir, "--auto"}
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		if in.Workdir != "" {
			args = append(args, "--title", safeTitle(in.Workdir, "dhi"))
		}
		// No verified system-prompt flag on `run`: the system block
		// rides ahead of the prompt in the shared tagged shape (M14 P1;
		// live-verify checklist: switch to a native flag if one lands).
		args = append(args, PromptWithSystem(in.System, in.Prompt))
		return args
	},
	ParseStream: opencodeParseStream,
	Finalize:    opencodeFinalize,
	Version:     opencodeVersion,
	// StdinOK stays false: no verified stdin prompt path on `run` —
	// oversized prompts refuse by name (ADR-0011).
	//
	// MCPOK: OPENCODE_CONFIG points at a per-turn config that MERGES
	// into the user's global config (verified 2026-09-26 on 1.18.25:
	// `opencode debug config` lists both). So DHI's `dhi` server joins
	// the run while the user's providers/auth survive — but their own
	// global MCP servers stay visible too (no strict flag observed);
	// that containment caveat is carried on the doctor row.
	MCPOK:        true,
	MCPConfigEnv: "OPENCODE_CONFIG",
	MCPConfigFile: func(endpoint string) string {
		return fmt.Sprintf(
			`{"$schema":"https://opencode.ai/config.json","mcp":{"dhi":{"type":"remote","url":%q,"enabled":true}}}`,
			endpoint)
	},
}

// opencodeItem is the union shape of one run JSONL line.
type opencodeItem struct {
	Type string `json:"type"`
	Part struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		Tool   string `json:"tool"`
		Title  string `json:"title"`
		Reason string `json:"reason"`
		State  struct {
			Status string `json:"status"`
			Input  struct {
				Command string `json:"command"`
			} `json:"input"`
			Metadata struct {
				Exit int `json:"exit"`
			} `json:"metadata"`
		} `json:"state"`
		Tokens struct {
			Total  int `json:"total"`
			Input  int `json:"input"`
			Output int `json:"output"`
			Reason int `json:"reasoning"`
		} `json:"tokens"`
		Cost float64 `json:"cost"`
	} `json:"part"`
}

// opencodeStop is the normalized terminal payload the parser hands
// Finalize: tokens + cost from the stop step_finish, last_message from
// the final text event.
type opencodeStop struct {
	Type        string  `json:"type"` // "opencode.step_finish"
	LastMessage string  `json:"last_message"`
	Cost        float64 `json:"cost"`
	Tokens      struct {
		Input  int `json:"input"`
		Output int `json:"output"`
	} `json:"tokens"`
}

func opencodeParseStream(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		var lastText string
		for line := range lineStream(r) {
			var it opencodeItem
			if err := json.Unmarshal([]byte(line), &it); err != nil {
				ch <- StreamEvent{Kind: EventError, Detail: truncate(err.Error(), 120)}
				continue
			}
			switch it.Type {
			case "step_finish":
				if it.Part.Reason == "stop" {
					stop := opencodeStop{Type: "opencode.step_finish", LastMessage: lastText, Cost: it.Part.Cost}
					stop.Tokens.Input = it.Part.Tokens.Input
					stop.Tokens.Output = it.Part.Tokens.Output
					b, err := json.Marshal(stop)
					if err != nil {
						ch <- StreamEvent{Kind: EventError, Detail: "opencode: marshal final"}
						continue
					}
					ch <- StreamEvent{Kind: EventFinal, Detail: string(b)}
				}
			case "text":
				if strings.TrimSpace(it.Part.Text) != "" {
					lastText = it.Part.Text
					ch <- StreamEvent{Kind: EventProgress, Detail: truncate(it.Part.Text, 200)}
				}
			case "tool_use":
				title := it.Part.Title
				if title == "" {
					title = it.Part.State.Input.Command
				}
				if title == "" {
					title = it.Part.Tool
				}
				if it.Part.State.Status == "error" || it.Part.State.Metadata.Exit != 0 {
					ch <- StreamEvent{Kind: EventError, Detail: fmt.Sprintf(
						"tool %s failed: %s", it.Part.Tool, truncate(title, 160))}
					continue
				}
				ch <- StreamEvent{Kind: EventCommand, Detail: "tool: " + truncate(title, 160)}
			}
		}
	}()
	return ch
}

// opencodeFinalize parses the normalized terminal payload: tokens (when
// reported) and cost (0 = provider reported none → HasCost false). The
// final summary is the assistant's last text.
func opencodeFinalize(final string) (string, Usage, error) {
	var stop opencodeStop
	if err := json.Unmarshal([]byte(final), &stop); err != nil {
		return "", Usage{}, fmt.Errorf("clirun/opencode: final: %w", err)
	}
	u := Usage{
		TokensIn:  stop.Tokens.Input,
		TokensOut: stop.Tokens.Output,
		CostUSD:   stop.Cost,
		HasCost:   stop.Cost > 0,
	}
	if stop.Tokens.Input == 0 {
		u.TokensIn = -1
	}
	if stop.Tokens.Output == 0 {
		u.TokensOut = -1
	}
	return strings.TrimSpace(stop.LastMessage), u, nil
}

// opencodeVersion extracts the version token from `opencode --version`
// ("1.18.25").
func opencodeVersion(ctx context.Context, path string) (string, error) {
	out, err := probeVersion(ctx, path)
	if err != nil {
		return "", err
	}
	if v := firstVersionToken(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("clirun/opencode: no version in %q", truncate(out, 60))
}
