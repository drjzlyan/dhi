package clirun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// antigravityFixture is a real captured stream (agy 1.2.11): init, a
// tool step (run_command), the agent's text response, then the terminal
// result with usage.
const antigravityFixture = `{"event":"init","conversation_id":"c1","init":{"cwd":"/tmp","tools":["run_command"],"permission_mode":"always-proceed"}}
{"event":"step_update","step_update":{"conversation_id":"c1","step_index":0,"state":"DONE","step_type":"user_input"}}
{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"DONE","step_type":"agent_response","duration_seconds":3.6}}
{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"echo HELLO_AGY"}}}}
{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"DONE","step_type":"tool","tool_name":"run_command","duration_seconds":0.29,"tool_info":{"name":"run_command","parameters":{"CommandLine":"echo HELLO_AGY"},"output":"HELLO_AGY\r\n"}}}
{"event":"step_update","step_update":{"conversation_id":"c1","step_index":3,"state":"ACTIVE","step_type":"agent_response","text_delta":"HELLO_AGY"}}
{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"HELLO_AGY\n","duration_seconds":7.0,"num_turns":1,"usage":{"input_tokens":24928,"output_tokens":425,"thinking_tokens":360,"cache_read_tokens":0,"total_tokens":25353}}}
`

func TestAntigravityBuildArgs(t *testing.T) {
	got := Antigravity.BuildArgs(RunInput{Prompt: "summarize", Model: "gemini-3-pro", Workdir: "/w"})
	want := []string{
		"-p", "summarize",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
		"--model", "gemini-3-pro",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestAntigravityParseStreamAndFinalize(t *testing.T) {
	events := parse(Antigravity, antigravityFixture)
	var final string
	var progress, commands []string
	for _, e := range events {
		switch e.Kind {
		case EventProgress:
			progress = append(progress, e.Detail)
		case EventCommand:
			commands = append(commands, e.Detail)
		case EventFinal:
			final = e.Detail
		}
	}
	if final == "" {
		t.Fatal("no final event")
	}
	if len(progress) != 1 || progress[0] != "HELLO_AGY" {
		t.Fatalf("progress = %v", progress)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "echo HELLO_AGY") {
		t.Fatalf("commands = %v", commands)
	}
	sum, u, err := Antigravity.Finalize(final)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sum != "HELLO_AGY" {
		t.Fatalf("summary = %q", sum)
	}
	if u.TokensIn != 24928 || u.TokensOut != 425 {
		t.Fatalf("usage = %+v", u)
	}
	if u.HasCost || u.CostUSD != 0 {
		t.Fatalf("antigravity must have no cost: %+v", u)
	}
}

func TestAntigravityResultFailureFails(t *testing.T) {
	fix := `{"event":"step_update","step_update":{"state":"ACTIVE","step_type":"agent_response","text_delta":"trying"}}
{"event":"result","result":{"status":"FAILURE","response":"","usage":{"input_tokens":5,"output_tokens":1}}}`
	var final string
	for _, e := range parse(Antigravity, fix) {
		if e.Kind == EventFinal {
			final = e.Detail
		}
	}
	if _, _, err := Antigravity.Finalize(final); err == nil {
		t.Fatal("non-SUCCESS result must fail Finalize")
	}
}

func TestAntigravityLiveVerified(t *testing.T) {
	if Antigravity.Tested == "" {
		t.Fatal("antigravity was live-verified on 1.2.11; Tested must be set")
	}
	if !containsStr(Antigravity.EnvPass, "HOME") {
		t.Fatalf("EnvPass must declare HOME so the CLI reaches its OAuth state: %v", Antigravity.EnvPass)
	}
	if Antigravity.StdinOK {
		t.Fatal("antigravity stdin is NDJSON, not a raw prompt; StdinOK must be false")
	}
}

func TestAntigravityVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "agy", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"1.2.11\"; fi\n")
	v, err := Antigravity.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "1.2.11" {
		t.Fatalf("version = %q", v)
	}
}

func TestAntigravityGeminiDirMirror(t *testing.T) {
	realGemini := t.TempDir()
	if err := os.MkdirAll(filepath.Join(realGemini, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(realGemini, "oauth_creds.json"), []byte("tok"), 0o600))
	must(os.WriteFile(filepath.Join(realGemini, "config", "settings.json"), []byte("{}"), 0o644))
	must(os.WriteFile(filepath.Join(realGemini, "config", "mcp_config.json"), []byte(`{"mcpServers":{"user":{}}}`), 0o644))
	must(os.MkdirAll(filepath.Join(realGemini, "antigravity-cli"), 0o755))

	base := t.TempDir()
	dir, cleanup, err := antigravityMCPGeminiDir(realGemini, base, Antigravity.MCPConfigFile("http://127.0.0.1:9/mcp"))
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	defer cleanup()

	// OAuth + state are symlinks back to the real dir (auth survives).
	if ln, err := os.Readlink(filepath.Join(dir, "oauth_creds.json")); err != nil || ln != filepath.Join(realGemini, "oauth_creds.json") {
		t.Fatalf("oauth symlink = %q err=%v", ln, err)
	}
	if ln, err := os.Readlink(filepath.Join(dir, "antigravity-cli")); err != nil || ln != filepath.Join(realGemini, "antigravity-cli") {
		t.Fatalf("state symlink = %q err=%v", ln, err)
	}
	// Non-mcp config entries are symlinked through.
	if ln, err := os.Readlink(filepath.Join(dir, "config", "settings.json")); err != nil || ln != filepath.Join(realGemini, "config", "settings.json") {
		t.Fatalf("settings symlink = %q err=%v", ln, err)
	}
	// mcp_config.json is a real file carrying DHI's endpoint, not the user's.
	if fi, err := os.Lstat(filepath.Join(dir, "config", "mcp_config.json")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("mcp_config.json must be a real file: %v %v", fi, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "config", "mcp_config.json"))
	if err != nil || !strings.Contains(string(body), "127.0.0.1:9") {
		t.Fatalf("mcp body = %q err=%v", body, err)
	}
	// cleanup removes the mirror.
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("mirror survived cleanup: %v", err)
	}
}

func TestAntigravityMCPArgs(t *testing.T) {
	args := strings.Join(Antigravity.BuildArgs(RunInput{Prompt: "p", MCPGeminiDir: "/x/gemini"}), "\x00")
	if !strings.Contains(args, "--gemini_dir=/x/gemini") {
		t.Fatalf("antigravity argv missing --gemini_dir: %q", args)
	}
	if strings.Contains(strings.Join(Antigravity.BuildArgs(RunInput{Prompt: "p"}), "\x00"), "--gemini_dir") {
		t.Fatal("antigravity emitted --gemini_dir without a dir")
	}
}
