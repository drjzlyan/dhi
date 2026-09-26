package clirun

import (
	"context"
	"strings"
	"testing"
)

// copilotFixture matches the LIVE-CAPTURED JSONL envelope (copilot
// 1.0.88): assistant.message with toolRequests, tool.execution_start /
// tool.execution_complete, and a terminal `result` carrying exitCode.
const copilotFixture = `{"type":"session.auto_mode_resolved","data":{"chosenModel":"gpt-6-luna"}}
{"type":"user.message","data":{"content":"how many go files?"}}
{"type":"assistant.message","data":{"content":"Let me look.","toolRequests":[{"name":"bash","arguments":{"command":"find . -name '*.go'"}}]}}
{"type":"tool.execution_start","data":{"toolName":"bash","arguments":{"command":"find . -name '*.go'"}}}
{"type":"tool.execution_complete","data":{"toolName":"bash","success":true,"result":{"content":"3 files"}}}
{"type":"assistant.message","data":{"content":"Found 3 Go files."}}
{"type":"result","exitCode":0,"usage":{"premiumRequests":1}}
`

func TestCopilotBuildArgs(t *testing.T) {
	got := Copilot.BuildArgs(RunInput{Prompt: "summarize", Model: "gpt-5", Workdir: "/w"})
	want := []string{
		"-p", "summarize",
		"-s",
		"--no-ask-user",
		"--output-format=json",
		"--allow-all-tools",
		"--model", "gpt-5",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestCopilotBuildArgsMCP(t *testing.T) {
	got := Copilot.BuildArgs(RunInput{Prompt: "p", MCPURL: "http://127.0.0.1:1/mcp"})
	if !containsStr(got, "--disable-builtin-mcps") {
		t.Fatalf("MCP run must drop builtin servers for containment: %v", got)
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
	if u.TokensIn != -1 || u.TokensOut != -1 || u.HasCost {
		t.Fatalf("copilot reports no token total here → unknown, no cost: %+v", u)
	}
}

func TestCopilotToolFailure(t *testing.T) {
	fix := `{"type":"assistant.message","data":{"content":"running"}}
{"type":"tool.execution_complete","data":{"toolName":"bash","success":false,"result":{"content":"exit 2 boom"}}}
{"type":"result","exitCode":0}`
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

func TestCopilotNonZeroExitFails(t *testing.T) {
	fix := `{"type":"assistant.message","data":{"content":"working"}}
{"type":"result","exitCode":1}`
	var final string
	for _, e := range parse(Copilot, fix) {
		if e.Kind == EventFinal {
			final = e.Detail
		}
	}
	if _, _, err := Copilot.Finalize(final); err == nil {
		t.Fatal("non-zero exit must fail Finalize")
	}
}

func TestCopilotLiveVerified(t *testing.T) {
	if Copilot.Tested == "" {
		t.Fatal("copilot was live-verified on 1.0.88; Tested must be set")
	}
	if !containsStr(Copilot.EnvPass, "COPILOT_GITHUB_TOKEN") {
		t.Fatalf("EnvPass must declare COPILOT_GITHUB_TOKEN: %v", Copilot.EnvPass)
	}
	if Copilot.MCPProjectFile != ".mcp.json" {
		t.Fatalf("copilot MCP delivery must be a project file: %q", Copilot.MCPProjectFile)
	}
}

func TestCopilotVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "copilot", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"GitHub Copilot CLI 1.0.88.\"; fi\n")
	v, err := Copilot.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "1.0.88" {
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
