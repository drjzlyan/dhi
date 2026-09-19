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
	Tested: "", // pinned once live-verified (not installed 2026-09-09)
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
		}
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		return args
	},
	ParseStream: copilotParseStream,
	Finalize:    copilotFinalize,
	Version:     copilotVersion,
}

// copilotItem is the union shape of one JSONL envelope line.
type copilotItem struct {
	Type string `json:"type"`
	Data struct {
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
		Result       any    `json:"result"`
		Usage        any    `json:"usage"`
	} `json:"data"`
	Error any `json:"error"`
}

// copilotStop is the normalized terminal payload the parser hands
// Finalize: last assistant message + any reported tokens (copilot has
// no cost).
type copilotStop struct {
	Type        string `json:"type"` // "copilot.session.termination"
	LastMessage string `json:"last_message"`
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
			case "session.termination", "session.shutdown":
				stop := copilotStop{Type: "copilot.session.termination", LastMessage: lastText}
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
					name := tr.Name
					if name == "" {
						name = tr.Arguments.Command
					}
					if name != "" {
						ch <- StreamEvent{Kind: EventCommand, Detail: "tool: " + truncate(name, 160)}
					}
				}
			case "tool.execution_end":
				if it.Data.Result != nil {
					if s, ok := it.Data.Result.(string); ok && !strings.EqualFold(s, "success") && !strings.Contains(s, "success") {
						ch <- StreamEvent{Kind: EventError, Detail: "tool " + it.Data.Name + ": " + truncate(s, 160)}
					}
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
	if v := firstVersionToken(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("clirun/copilot: no version in %q", truncate(out, 60))
}
