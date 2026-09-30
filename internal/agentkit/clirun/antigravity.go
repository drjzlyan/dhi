// Antigravity adapter (F-013; replaces the deprecated Gemini CLI).
//
// Binary:   agy  (Google Antigravity CLI)
// Invoked:  agy -p <prompt> --output-format stream-json
//
//	--dangerously-skip-permissions [-m M]
//
// stream-json contract — LIVE-VERIFIED 2026-09-26 on agy 1.2.11
// (captured from real runs, not documented-only):
//
//	{"event":"init", "init":{cwd,tools,permission_mode}}
//	{"event":"step_update","step_update":{state,step_type,text_delta,
//	    tool_name,tool_info:{parameters:{CommandLine},output},usage}}
//	    step_type: user_input | agent_response | tool
//	{"event":"result","result":{status,response,usage:{input_tokens,
//	    output_tokens,...}}}
//
// There is no native system-prompt flag, so the system block rides ahead
// of the prompt in the shared tagged shape. `--print` is a single-turn
// headless mode; stdin is NDJSON (not a raw prompt), so StdinOK=false.
//
// MCP — LIVE-VERIFIED 2026-09-30 on agy 1.2.11: agy reads servers only
// from <gemini_dir>/config/mcp_config.json and takes the dir via the
// `--gemini_dir` flag. The runtime builds a per-turn mirror of the
// user's ~/.gemini (symlinked except config/mcp_config.json) so OAuth +
// conversation state survive while DHI's loopback endpoint is injected.
// `agy --gemini_dir=<mirror> -p ... call_mcp_tool` reached DHI's tool.
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

// Antigravity is the Antigravity CLI adapter. Registered under the
// runtime value "antigravity".
var Antigravity = &CLI{
	Name:   "antigravity",
	Bin:    "agy",
	Tested: "1.2.11", // live-verified 2026-09-26
	// EXACT declared pass-through: HOME/XDG so the CLI reaches its own
	// OAuth state; nothing else crosses (ADR-0012 §4).
	EnvPass: []string{
		"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
		"GOOGLE_APPLICATION_CREDENTIALS",
	},
	StateRoot: func() []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return []string{
			filepath.Join(home, ".gemini", "antigravity-cli"),
			filepath.Join(home, ".cache", "antigravity"),
		}
	},
	BuildArgs: func(in RunInput) []string {
		args := []string{
			"-p", PromptWithSystem(in.System, in.Prompt),
			"--output-format", "stream-json",
			"--dangerously-skip-permissions", // OS sandbox is the boundary
		}
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		if in.MCPGeminiDir != "" {
			args = append(args, "--gemini_dir="+in.MCPGeminiDir)
		}
		return args
	},
	ParseStream: antigravityParseStream,
	Finalize:    antigravityFinalize,
	Version:     antigravityVersion,
	// MCP: agy reads servers only from <gemini_dir>/config/mcp_config.json
	// and offers no config-path flag — the runtime points --gemini_dir at
	// a per-turn mirror (MCPGeminiDir) holding this file.
	MCPOK:            true,
	MCPConfigFile:    antigravityMCPConfig,
	MCPGeminiDir:     antigravityMCPGeminiDirForHome,
	MCPGeminiDirFlag: "--gemini_dir",
}

// antigravityMCPGeminiDirForHome resolves the user's real ~/.gemini and
// builds the per-turn mirror from it.
func antigravityMCPGeminiDirForHome(baseDir, configBody string) (string, func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, err
	}
	return antigravityMCPGeminiDir(filepath.Join(home, ".gemini"), baseDir, configBody)
}

// antigravityMCPConfig renders agy's mcp_config.json for DHI's loopback
// endpoint (http transport).
func antigravityMCPConfig(endpoint string) string {
	return fmt.Sprintf("{\n  \"mcpServers\": {\n    \"dhi\": {\n      \"url\": %q\n    }\n  }\n}\n", endpoint)
}

// antigravityMCPGeminiDir builds a per-turn --gemini_dir mirror: a
// directory symlinking every entry of realGeminiDir (so OAuth +
// conversation state keep working) except config/mcp_config.json,
// which is replaced with DHI's rendered config. The mirror lives under
// baseDir (an admitted runtime path); cleanup removes it.
func antigravityMCPGeminiDir(realGeminiDir, baseDir, configBody string) (string, func(), error) {
	real := realGeminiDir
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(baseDir, "gemini-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	linkAllExcept := func(src, dst, skip string) error {
		ents, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if e.Name() == skip {
				continue
			}
			if err := os.Symlink(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if err := linkAllExcept(real, dir, "config"); err != nil {
		cleanup()
		return "", nil, err
	}
	cfg := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := linkAllExcept(filepath.Join(real, "config"), cfg, "mcp_config.json"); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(cfg, "mcp_config.json"), []byte(configBody), 0o644); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

// antigravityLine is the union shape of one stream-json line.
type antigravityLine struct {
	Event      string `json:"event"`
	StepUpdate struct {
		State     string `json:"state"`
		StepType  string `json:"step_type"`
		TextDelta string `json:"text_delta"`
		ToolName  string `json:"tool_name"`
		ToolInfo  struct {
			Name       string `json:"name"`
			Parameters struct {
				CommandLine string `json:"CommandLine"`
			} `json:"parameters"`
			Output string `json:"output"`
		} `json:"tool_info"`
	} `json:"step_update"`
	Result struct {
		Status   string `json:"status"`
		Response string `json:"response"`
		Usage    struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"result"`
}

// antigravityStop is the normalized terminal payload Finalize consumes.
type antigravityStop struct {
	Status    string `json:"status"`
	Response  string `json:"response"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
}

func antigravityParseStream(r io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		for line := range lineStream(r) {
			var it antigravityLine
			if err := json.Unmarshal([]byte(line), &it); err != nil {
				ch <- StreamEvent{Kind: EventError, Detail: truncate(err.Error(), 120)}
				continue
			}
			switch it.Event {
			case "step_update":
				su := it.StepUpdate
				switch su.StepType {
				case "agent_response":
					if su.State == "ACTIVE" {
						if t := strings.TrimSpace(su.TextDelta); t != "" {
							ch <- StreamEvent{Kind: EventProgress, Detail: truncate(t, 200)}
						}
					}
				case "tool":
					if su.State == "ACTIVE" {
						label := su.ToolName
						if cmd := strings.TrimSpace(su.ToolInfo.Parameters.CommandLine); cmd != "" {
							label += ": " + cmd
						}
						if label != "" {
							ch <- StreamEvent{Kind: EventCommand, Detail: "tool: " + truncate(label, 160)}
						}
					}
				}
			case "result":
				stop := antigravityStop{
					Status: it.Result.Status, Response: it.Result.Response,
					TokensIn: it.Result.Usage.InputTokens, TokensOut: it.Result.Usage.OutputTokens,
				}
				b, err := json.Marshal(stop)
				if err != nil {
					ch <- StreamEvent{Kind: EventError, Detail: "clirun/antigravity: marshal final"}
					continue
				}
				ch <- StreamEvent{Kind: EventFinal, Detail: string(b)}
			case "error":
				ch <- StreamEvent{Kind: EventError, Detail: truncate(line, 160)}
			}
		}
	}()
	return ch
}

// antigravityFinalize: a non-SUCCESS result fails the run; tokens come
// from the result usage; cost stays absent (HasCost false).
func antigravityFinalize(final string) (string, Usage, error) {
	var stop antigravityStop
	if err := json.Unmarshal([]byte(final), &stop); err != nil {
		return "", Usage{}, fmt.Errorf("clirun/antigravity: final: %w", err)
	}
	if stop.Status != "" && stop.Status != "SUCCESS" {
		return "", Usage{}, fmt.Errorf("clirun/antigravity: run ended with status %q", stop.Status)
	}
	u := Usage{TokensIn: stop.TokensIn, TokensOut: stop.TokensOut}
	if stop.TokensIn == 0 {
		u.TokensIn = -1
	}
	if stop.TokensOut == 0 {
		u.TokensOut = -1
	}
	return strings.TrimSpace(stop.Response), u, nil
}

// antigravityVersion extracts the version token from `agy --version`.
func antigravityVersion(ctx context.Context, path string) (string, error) {
	out, err := probeVersion(ctx, path, "--version")
	if err != nil {
		return "", err
	}
	if v := firstVersionToken(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("clirun/antigravity: no version in %q", truncate(out, 60))
}
