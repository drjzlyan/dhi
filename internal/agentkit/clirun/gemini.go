// Gemini adapter (F-013 wave 3).
//
// Binary:   gemini  (gemini CLI; google-gemini/gemini-cli)
// Invoked:  gemini -p <prompt> --output-format stream-json --yolo
//
//	[-m M]
//
// Documented stream-json contract
// (google-gemini.github.io/gemini-cli/docs/cli/headless.html + the
// stream-json timeline PR):
//
//	init         → session metadata (model list); no transcript text
//	message      → role "assistant": text parts/tool calls
//	tool_use     → tool_name + parameters.command
//	tool_result  → the tool's return; error surfaced via status/error
//	error        → a run-level error event (stream usually continues to
//	               a result)
//	result       → terminal: status success/error + stats {tokens,
//	               tool_calls, duration_ms}
//
// NOTE — fixture-first (2026-09-09): gemini is NOT installed on the dev
// machine, so this adapter (parser + fixtures) is built to the
// documented contract above. LIVE-VERIFY CHECKLIST (fill before this
// adapter's doctor row may report OK):
//  1. `gemini --version` banner + extracted version token.
//  2. Exact headless flag set (`-p`, `--output-format stream-json`,
//     `--yolo`, `--model`) on a real run (non-interactive TTY-less).
//  3. Stream shapes: message text field, tool_use command param,
//     tool_result error surfacing, the terminal result event and its
//     stats.tokens shape (input_tokens/output_tokens).
//  4. Cost location: HasCost false unless live runs place a cost.
//  5. Env keys confirmed: GEMINI_API_KEY, GOOGLE_GENAI_USE_VERTEXAI,
//     GOOGLE_GENAI_USE_GCA.
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

// Gemini is the Gemini CLI adapter. Registered under the runtime value
// "gemini".
var Gemini = &CLI{
	Name:   "gemini",
	Bin:    "gemini",
	Tested: "", // pinned once live-verified (not installed 2026-09-09)
	// EXACT declared pass-through: provider credentials + the Vertex/GCA
	// toggles gemini honors. Nothing else crosses (ADR-0012 §4).
	EnvPass: []string{
		"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
		"GEMINI_API_KEY",
		"GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_GCA",
	},
	StateRoot: func() []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return []string{filepath.Join(home, ".gemini")}
	},
	BuildArgs: func(in RunInput) []string {
		args := []string{
			// No verified system-prompt flag: the system block rides
			// ahead of the prompt in the shared tagged shape (M14 P1;
			// live-verify checklist: switch to a native flag if one
			// lands). The prompt stays the first positional arg.
			"-p", PromptWithSystem(in.System, in.Prompt),
			"--output-format", "stream-json",
			"--yolo", // auto-approve tools; OS sandbox is the boundary
		}
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		return args
	},
	ParseStream: geminiParseStream,
	Finalize:    geminiFinalize,
	Version:     geminiVersion,
}

// geminiItem is the union shape of one stream-json line.
type geminiItem struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Status  string `json:"status"`
	Name    string `json:"name"` // tool name inside tool_use
	Error   string `json:"error"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Parameters struct {
		Command string `json:"command"`
	} `json:"parameters"`
	Delta string `json:"delta"`
	Stats struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"stats"`
}

// geminiStop is the normalized terminal payload the parser hands
// Finalize: the result event carries stats but no summary text, so the
// parser stitches the last assistant message onto it.
type geminiStop struct {
	Type        string `json:"type"` // "gemini.result"
	Status      string `json:"status"`
	LastMessage string `json:"last_message"`
	Tokens      struct {
		Input  int `json:"input"`
		Output int `json:"output"`
	} `json:"tokens"`
}

func geminiParseStream(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		var lastText string
		for line := range lineStream(r) {
			var it geminiItem
			if err := json.Unmarshal([]byte(line), &it); err != nil {
				ch <- StreamEvent{Kind: EventError, Detail: truncate(err.Error(), 120)}
				continue
			}
			switch it.Type {
			case "result":
				stop := geminiStop{Type: "gemini.result", Status: it.Status, LastMessage: lastText}
				stop.Tokens.Input = it.Stats.Tokens.InputTokens
				stop.Tokens.Output = it.Stats.Tokens.OutputTokens
				b, err := json.Marshal(stop)
				if err != nil {
					ch <- StreamEvent{Kind: EventError, Detail: "gemini: marshal final"}
					continue
				}
				ch <- StreamEvent{Kind: EventFinal, Detail: string(b)}
			case "message":
				text := strings.TrimSpace(it.Message.Text)
				if text == "" {
					text = strings.TrimSpace(it.Delta)
				}
				if text != "" && it.Role != "user" {
					lastText = text
					ch <- StreamEvent{Kind: EventProgress, Detail: truncate(text, 200)}
				}
			case "tool_use":
				name := it.Name
				if cmd := strings.TrimSpace(it.Parameters.Command); cmd != "" {
					name = cmd
				}
				if name != "" {
					ch <- StreamEvent{Kind: EventCommand, Detail: "tool: " + truncate(name, 160)}
				}
			case "tool_result":
				if it.Error != "" || (it.Status != "" && it.Status != "success") {
					ch <- StreamEvent{Kind: EventError, Detail: "tool failed: " + truncate(it.Error, 160)}
				}
			case "error":
				ch <- StreamEvent{Kind: EventError, Detail: truncate(it.Error, 160)}
			}
		}
	}()
	return ch
}

// geminiFinalize parses the stitched terminal payload: status error
// fails the run; token usage comes from stats when reported; cost stays
// absent (HasCost false) until live runs place one.
func geminiFinalize(final string) (string, Usage, error) {
	var stop geminiStop
	if err := json.Unmarshal([]byte(final), &stop); err != nil {
		return "", Usage{}, fmt.Errorf("clirun/gemini: final: %w", err)
	}
	if stop.Status != "" && stop.Status != "success" {
		return "", Usage{}, fmt.Errorf("clirun/gemini: run ended with status %q", stop.Status)
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

// geminiVersion extracts the version token from `gemini --version`.
func geminiVersion(ctx context.Context, path string) (string, error) {
	out, err := probeVersion(ctx, path, "--version")
	if err != nil {
		return "", err
	}
	if v := firstVersionToken(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("clirun/gemini: no version in %q", truncate(out, 60))
}
