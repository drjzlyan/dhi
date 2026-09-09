package clirun

import (
	"context"
	"strings"
	"testing"
)

// opencodeFixture is a faithful slice of the opencode run JSONL stream
// (captured live vs 1.18.25): multi-step turns with a bash tool, the
// stop step_finish carrying tokens + cost (0 when unconfigured).
const opencodeFixture = `{"type":"step_start","part":{"type":"step-start","sessionID":"s1"}}
{"type":"text","part":{"type":"text","text":"Let me check the current state."}}
{"type":"step_finish","part":{"reason":"tool-calls","tokens":{"total":1200,"input":1000,"output":200,"reasoning":0,"cache":{"write":0,"read":0}},"cost":0}}
{"type":"tool_use","part":{"type":"tool","tool":"bash","title":"base64 -d <<< 'aGk='","state":{"status":"completed","metadata":{"exit":0}}}}
{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Done: report.md written with the summary."}}
{"type":"step_finish","part":{"reason":"stop","tokens":{"total":1600,"input":1100,"output":500,"reasoning":0,"cache":{"write":0,"read":300}},"cost":0.012}}
`

func TestOpenCodeBuildArgs(t *testing.T) {
	c := OpenCode
	got := c.BuildArgs(RunInput{Prompt: "summarize", Model: "claude-sonnet-4-5", Workdir: "/ws/member"})
	want := []string{
		"run", "--format", "json", "--dir", "/ws/member", "--auto",
		"--model", "claude-sonnet-4-5",
		"--title", "dhi-member",
		"summarize",
	}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if c.Bin != "opencode" || c.Tested == "" || c.Name != "opencode" {
		t.Fatalf("adapter identity: %+v", c)
	}
}

func TestOpenCodeBuildArgsWithoutModel(t *testing.T) {
	got := OpenCode.BuildArgs(RunInput{Prompt: "p", Workdir: "/w"})
	for _, a := range got {
		if a == "--model" {
			t.Fatal("no --model expected when model empty")
		}
	}
}

func TestOpenCodeEnvAndStateRoot(t *testing.T) {
	env := map[string]bool{}
	for _, k := range OpenCode.EnvPass {
		env[k] = true
	}
	for _, k := range []string{"OPENCODE_CONFIG", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		if !env[k] {
			t.Errorf("EnvPass missing %s", k)
		}
	}
	if len(OpenCode.StateRoot()) == 0 {
		t.Fatal("StateRoot must resolve")
	}
}

func TestOpenCodeParseStreamAndFinalize(t *testing.T) {
	events := parse(OpenCode, opencodeFixture)
	var final string
	var progress []string
	var commands []string
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
	if len(progress) != 2 || progress[0] != "Let me check the current state." || progress[1] != "Done: report.md written with the summary." {
		t.Fatalf("progress = %v", progress)
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "tool: base64") {
		t.Fatalf("commands = %v", commands)
	}
	sum, u, err := OpenCode.Finalize(final)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sum != "Done: report.md written with the summary." {
		t.Fatalf("summary = %q", sum)
	}
	if u.TokensIn != 1100 || u.TokensOut != 500 {
		t.Fatalf("usage = %+v", u)
	}
	if u.CostUSD != 0.012 || !u.HasCost {
		t.Fatalf("cost = %+v", u)
	}
}

func TestOpenCodeZeroCost(t *testing.T) {
	fix := `{"type":"text","part":{"type":"text","text":"ok"}}
{"type":"step_finish","part":{"reason":"stop","tokens":{"total":900,"input":800,"output":100},"cost":0}}`
	var final string
	for _, e := range parse(OpenCode, fix) {
		if e.Kind == EventFinal {
			final = e.Detail
		}
	}
	_, u, err := OpenCode.Finalize(final)
	if err != nil {
		t.Fatal(err)
	}
	if u.HasCost || u.CostUSD != 0 {
		t.Fatalf("cost must be absent when CLI reports 0: %+v", u)
	}
}

func TestOpenCodeToolFailure(t *testing.T) {
	fix := `{"type":"tool_use","part":{"type":"tool","tool":"bash","title":"make build","state":{"status":"completed","metadata":{"exit":2}}}}
{"type":"text","part":{"type":"text","text":"x"}}
{"type":"step_finish","part":{"reason":"stop","tokens":{"input":5,"output":1},"cost":0}}`
	var errs []string
	for _, e := range parse(OpenCode, fix) {
		if e.Kind == EventError {
			errs = append(errs, e.Detail)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "failed") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestOpenCodeParseStreamNoise(t *testing.T) {
	fix := `not json
{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"done"}}
{"type":"step_finish","part":{"reason":"stop","tokens":{"input":4,"output":2},"cost":0}}`
	events := parse(OpenCode, fix)
	var finals, errs int
	for _, e := range events {
		switch e.Kind {
		case EventFinal:
			finals++
		case EventError:
			errs++
		}
	}
	if finals != 1 {
		t.Fatalf("finals = %d", finals)
	}
	if errs != 1 {
		t.Fatalf("errs = %d (%v)", errs, events)
	}
}

func TestOpenCodeVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "opencode", opencodeVersionStub)
	v, err := OpenCode.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "1.18.25" {
		t.Fatalf("version = %q", v)
	}
}

const opencodeVersionStub = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "1.18.25"; exit 0; fi
exit 1`
