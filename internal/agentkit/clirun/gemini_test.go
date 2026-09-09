package clirun

import (
	"context"
	"strings"
	"testing"
)

// geminiFixture matches the documented stream-json timeline
// (init/message/tool_use/tool_result/error/result): init, a user turn,
// assistant text, a shell tool, then the terminal result with stats.
const geminiFixture = `{"type":"init","details":{"config":{"model":"gemini-2.0-flash"}}}
{"type":"message","role":"user","message":{"text":"how many files?"}}
{"type":"message","role":"assistant","message":{"text":"Let me list them."}}
{"type":"tool_use","name":"shell","parameters":{"command":"ls | wc -l"}}
{"type":"tool_result","status":"success","error":"","message":{"text":"3"}}
{"type":"message","role":"assistant","message":{"text":"There are 3 files."}}
{"type":"result","status":"success","stats":{"duration_ms":900,"tokens":{"input_tokens":220,"output_tokens":40},"tool_calls":1}}
`

func TestGeminiBuildArgs(t *testing.T) {
	got := Gemini.BuildArgs(RunInput{Prompt: "summarize", Model: "gemini-2.0-flash", Workdir: "/w"})
	want := []string{
		"-p", "summarize",
		"--output-format", "stream-json",
		"--yolo",
		"--model", "gemini-2.0-flash",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestGeminiParseStreamAndFinalize(t *testing.T) {
	events := parse(Gemini, geminiFixture)
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
	want := []string{"Let me list them.", "There are 3 files."}
	if strings.Join(progress, "|") != strings.Join(want, "|") {
		t.Fatalf("progress = %v", progress)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "ls | wc -l") {
		t.Fatalf("commands = %v", commands)
	}
	sum, u, err := Gemini.Finalize(final)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sum != "There are 3 files." {
		t.Fatalf("summary = %q", sum)
	}
	if u.TokensIn != 220 || u.TokensOut != 40 {
		t.Fatalf("usage = %+v", u)
	}
	if u.HasCost || u.CostUSD != 0 {
		t.Fatalf("gemini must have no cost: %+v", u)
	}
}

func TestGeminiResultErrorFails(t *testing.T) {
	fix := `{"type":"message","role":"assistant","message":{"text":"attempting"}}
{"type":"result","status":"error","stats":{"tokens":{"input_tokens":5,"output_tokens":1}}}`
	var final string
	for _, e := range parse(Gemini, fix) {
		if e.Kind == EventFinal {
			final = e.Detail
		}
	}
	if _, _, err := Gemini.Finalize(final); err == nil {
		t.Fatal("non-success result must fail Finalize")
	}
}

func TestGeminiToolResultError(t *testing.T) {
	fix := `{"type":"tool_use","name":"shell","parameters":{"command":"make build"}}
{"type":"tool_result","status":"error","error":"borked","message":{"text":""}}
{"type":"message","role":"assistant","message":{"text":"failed"}}
{"type":"result","status":"success","stats":{"tokens":{"input_tokens":3,"output_tokens":1}}}`
	var errs []string
	for _, e := range parse(Gemini, fix) {
		if e.Kind == EventError {
			errs = append(errs, e.Detail)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "borked") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestGeminiLiveVerifyGating(t *testing.T) {
	if Gemini.Tested != "" {
		t.Fatalf("Tested must be empty pre-live-verification, got %q", Gemini.Tested)
	}
	if len(Gemini.EnvPass) == 0 || !containsStr(Gemini.EnvPass, "GEMINI_API_KEY") {
		t.Fatalf("EnvPass must declare GEMINI_API_KEY: %v", Gemini.EnvPass)
	}
}

func TestGeminiVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "gemini", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"v0.9.5\"; fi\n")
	v, err := Gemini.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "0.9.5" {
		t.Fatalf("version = %q (want v-stripped token)", v)
	}
}
