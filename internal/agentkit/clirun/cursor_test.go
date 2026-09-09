package clirun

import (
	"context"
	"strings"
	"testing"
)

// cursorFixture matches the documented stream-json contract
// (cursor.com/docs/cli/headless): assistant text, started/completed
// tool_calls, terminal result carrying only a duration.
const cursorFixture = `{"type":"system","subtype":"init","model":"cursor-small"}
{"type":"assistant","text":"Checking the repo state."}
{"type":"tool_call","subtype":"started","tool":"read_file","title":"read_file: README.md","error":""}
{"type":"tool_call","subtype":"completed","tool":"read_file","title":"read_file: README.md","error":""}
{"type":"assistant","text":"Done: README is two paragraphs."}
{"type":"result","duration":1200}
`

func TestCursorBuildArgs(t *testing.T) {
	got := CursorAgent.BuildArgs(RunInput{Prompt: "summarize", Model: "cursor-small", Workdir: "/ws/member"})
	want := []string{
		"-p", "summarize",
		"--output-format", "stream-json",
		"--force",
		"--trust",
		"--model", "cursor-small",
		"--workspace", "/ws/member",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	if CursorAgent.Bin != "cursor-agent" || CursorAgent.Name != "cursor-agent" {
		t.Fatalf("adapter identity: %+v", CursorAgent)
	}
}

func TestCursorParseStreamAndFinalize(t *testing.T) {
	events := parse(CursorAgent, cursorFixture)
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
	want := []string{"Checking the repo state.", "Done: README is two paragraphs."}
	if strings.Join(progress, "|") != strings.Join(want, "|") {
		t.Fatalf("progress = %v", progress)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "read_file") {
		t.Fatalf("commands = %v", commands)
	}
	sum, u, err := CursorAgent.Finalize(final)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sum != "Done: README is two paragraphs." {
		t.Fatalf("summary = %q", sum)
	}
	if u.TokensIn != -1 || u.TokensOut != -1 || u.HasCost {
		t.Fatalf("usage must be unknown until live verification: %+v", u)
	}
}

func TestCursorToolError(t *testing.T) {
	fix := `{"type":"assistant","text":"trying"}
{"type":"tool_call","subtype":"started","tool":"write_file","title":"write_file: x.txt","error":""}
{"type":"tool_call","subtype":"completed","tool":"write_file","title":"write_file: x.txt","error":"permission denied"}
{"type":"result","duration":10}
`
	var errs []string
	for _, e := range parse(CursorAgent, fix) {
		if e.Kind == EventError {
			errs = append(errs, e.Detail)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "permission denied") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestCursorLiveVerifyGating(t *testing.T) {
	// Fixture-first: Tested must stay empty until the live-verify
	// checklist in cursor.go is filled in.
	if CursorAgent.Tested != "" {
		t.Fatalf("Tested must be empty pre-live-verification, got %q", CursorAgent.Tested)
	}
}

func TestCursorVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "cursor-agent", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"cursor-agent 1.2.3\"; fi\n")
	v, err := CursorAgent.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "1.2.3" {
		t.Fatalf("version = %q", v)
	}
}
