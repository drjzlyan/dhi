package clirun

import (
	"context"
	"strings"
	"testing"
)

// cursorFixture matches the LIVE-CAPTURED stream-json contract
// (cursor-agent 2026.09.26): nested assistant message.content[], thinking
// deltas on their own subtype, tool_call started/completed, and a
// terminal result carrying the final text + camelCase usage.
const cursorFixture = `{"type":"system","subtype":"init","model":"cursor-small"}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"do it"}]}}
{"type":"thinking","subtype":"delta","text":"Planning the reply."}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Checking the repo state."}]}}
{"type":"tool_call","subtype":"started","tool":"read_file","title":"read_file: README.md","error":""}
{"type":"tool_call","subtype":"completed","tool":"read_file","title":"read_file: README.md","error":""}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done: README is two paragraphs."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Done: README is two paragraphs.","usage":{"inputTokens":13269,"outputTokens":34}}
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

func TestCursorBuildArgsMCP(t *testing.T) {
	got := CursorAgent.BuildArgs(RunInput{Prompt: "p", MCPURL: "http://127.0.0.1:1/mcp"})
	if !containsStr(got, "--approve-mcps") {
		t.Fatalf("MCP run must auto-approve servers: %v", got)
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
	if u.TokensIn != 13269 || u.TokensOut != 34 || u.HasCost {
		t.Fatalf("usage = %+v", u)
	}
}

func TestCursorToolError(t *testing.T) {
	fix := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"trying"}]}}
{"type":"tool_call","subtype":"started","tool":"write_file","title":"write_file: x.txt","error":""}
{"type":"tool_call","subtype":"completed","tool":"write_file","title":"write_file: x.txt","error":"permission denied"}
{"type":"result","is_error":false,"result":"failed","usage":{}}
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

func TestCursorResultErrorFails(t *testing.T) {
	fix := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}
{"type":"result","is_error":true,"result":"","usage":{}}`
	var final string
	for _, e := range parse(CursorAgent, fix) {
		if e.Kind == EventFinal {
			final = e.Detail
		}
	}
	if _, _, err := CursorAgent.Finalize(final); err == nil {
		t.Fatal("is_error result must fail Finalize")
	}
}

func TestCursorLiveVerified(t *testing.T) {
	if CursorAgent.Tested == "" {
		t.Fatal("cursor-agent was live-verified on 2026.09.26; Tested must be set")
	}
	if CursorAgent.MCPProjectFile != ".cursor/mcp.json" {
		t.Fatalf("cursor MCP delivery must be a project file: %q", CursorAgent.MCPProjectFile)
	}
}

func TestCursorVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "cursor-agent", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"2026.09.26-dd393fe\"; fi\n")
	v, err := CursorAgent.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "2026.09.26" {
		t.Fatalf("version = %q", v)
	}
}
