package clirun

import (
	"context"
	"strings"
	"testing"
)

// copilotFixture matches the documented JSONL envelope (session-store
// shaped; github/copilot-cli#52): user/assistant messages with
// toolRequests, tool execution start/end, session.termination with
// usage.
const copilotFixture = `{"type":"session.start","data":{"id":"s1"}}
{"type":"user.message","data":{"content":"how many go files?"}}
{"type":"assistant.message","data":{"content":"Let me look.","outputTokens":20,"inputTokens":100,"toolRequests":[{"name":"find","arguments":{"command":"find . -name '*.go'"}}]}}
{"type":"tool.execution_start","data":{"name":"find","result":null}}
{"type":"tool.execution_end","data":{"name":"find","result":"success"}}
{"type":"assistant.message","data":{"content":"Found 3 Go files.","outputTokens":12,"inputTokens":30}}
{"type":"session.termination","data":{"inputTokens":150,"outputTokens":35}}
`

func TestCopilotBuildArgs(t *testing.T) {
	got := Copilot.BuildArgs(RunInput{Prompt: "summarize", Model: "gpt-5", Workdir: "/w"})
	want := []string{
		"-p", "summarize",
		"-s",
		"--no-ask-user",
		"--output-format=json",
		"--model", "gpt-5",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestCopilotParseStreamAndFinalize(t *testing.T) {
	events := parse(Copilot, copilotFixture)
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
	want := []string{"Let me look.", "Found 3 Go files."}
	if strings.Join(progress, "|") != strings.Join(want, "|") {
		t.Fatalf("progress = %v", progress)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "find") {
		t.Fatalf("commands = %v", commands)
	}
	sum, u, err := Copilot.Finalize(final)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sum != "Found 3 Go files." {
		t.Fatalf("summary = %q", sum)
	}
	if u.TokensIn != 150 || u.TokensOut != 35 {
		t.Fatalf("usage = %+v", u)
	}
	if u.HasCost || u.CostUSD != 0 {
		t.Fatalf("copilot must have no cost: %+v", u)
	}
}

func TestCopilotToolFailure(t *testing.T) {
	fix := `{"type":"assistant.message","data":{"content":"running"}}
{"type":"tool.execution_end","data":{"name":"make","result":"error: exit 2 boom"}}
{"type":"assistant.message","data":{"content":"oops"}}
{"type":"session.termination","data":{}}`
	var errs []string
	for _, e := range parse(Copilot, fix) {
		if e.Kind == EventError {
			errs = append(errs, e.Detail)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "exit 2") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestCopilotLiveVerifyGating(t *testing.T) {
	if Copilot.Tested != "" {
		t.Fatalf("Tested must be empty pre-live-verification, got %q", Copilot.Tested)
	}
	if len(Copilot.EnvPass) == 0 || !containsStr(Copilot.EnvPass, "COPILOT_GITHUB_TOKEN") {
		t.Fatalf("EnvPass must declare COPILOT_GITHUB_TOKEN: %v", Copilot.EnvPass)
	}
}

func TestCopilotVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "copilot", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"0.1.7\"; fi\n")
	v, err := Copilot.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "0.1.7" {
		t.Fatalf("version = %q", v)
	}
}

func containsStr(ss []string, sub string) bool {
	for _, s := range ss {
		if s == sub {
			return true
		}
	}
	return false
}
