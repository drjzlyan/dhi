package clirun

import (
	"context"
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
