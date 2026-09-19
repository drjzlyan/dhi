package clirun

import (
	"context"
	"strings"
	"testing"
)

// codexFixture is a faithful slice of the codex exec JSONL stream
// (captured live vs 0.147.0): item.started/completed for an agent
// message and a failed command, then turn.completed with usage.
const codexFixture = `{"type":"thread.started","timestamp":1788934000000,"thread_id":"t-1"}
{"type":"item.started","timestamp":1788934000100,"item":{"type":"agent_message","id":"m1","parent_id":null,"agent":"codex","role":"assistant"}}
{"type":"item.completed","timestamp":1788934000150,"item":{"type":"agent_message","id":"m1","parent_id":null,"agent":"codex","role":"assistant","text":"Let me check the current state."}}
{"type":"item.started","timestamp":1788934000200,"item":{"type":"command_execution","id":"c1","parent_id":null,"agent":"codex","role":"assistant","command":"base64 -d <<< 'aGk='"}}
{"type":"item.completed","timestamp":1788934000300,"item":{"type":"command_execution","id":"c1","parent_id":null,"agent":"codex","role":"assistant","command":"base64 -d <<< 'aGk='","status":"completed","exit_code":0,"output":"hi\n"}}
{"type":"item.completed","timestamp":1788934000400,"item":{"type":"agent_message","id":"m2","parent_id":null,"agent":"codex","role":"assistant","text":"Done: created report.md with the summary."}}
{"type":"turn.completed","timestamp":1788934000500,"thread_id":"t-1","turn_id":"x","usage":{"input_tokens":2317,"cached_input_tokens":110,"cache_write_input_tokens":0,"cache_read_input_tokens":110,"output_tokens":512,"reasoning_output_tokens":40}}
`

func TestCodexBuildArgs(t *testing.T) {
	c := Codex
	in := RunInput{Prompt: "fix the bug", System: "sys", Model: "gpt-5", Workdir: "/ws/member"}
	got := c.BuildArgs(in)
	// M14 P1: codex has no verified system flag, so the system block
	// rides ahead of the prompt in the shared tagged shape.
	want := []string{
		"exec", "--json", "--skip-git-repo-check", "-C", "/ws/member",
		"-m", "gpt-5",
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		"<dhi-system>\nsys\n</dhi-system>\n\nfix the bug",
	}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if c.Bin != "codex" || c.Tested == "" || c.Name != "codex" {
		t.Fatalf("adapter identity: %+v", c)
	}
	// Oversized delivery: the stdin shape drops the prompt arg for "-".
	stdin := c.BuildArgs(RunInput{Prompt: "", Stdin: "big blob", Workdir: "/ws/member"})
	if last := stdin[len(stdin)-1]; last != "-" {
		t.Fatalf("stdin argv tail = %q, want \"-\"", last)
	}
}

func TestCodexBuildArgsWithoutModel(t *testing.T) {
	got := Codex.BuildArgs(RunInput{Prompt: "p", Workdir: "/w"})
	for _, a := range got {
		if a == "-m" {
			t.Fatal("no -m expected when model empty")
		}
	}
}

func TestCodexEnvAndStateRoot(t *testing.T) {
	env := map[string]bool{}
	for _, k := range Codex.EnvPass {
		env[k] = true
	}
	for _, k := range []string{"CODEX_HOME", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		if !env[k] {
			t.Errorf("EnvPass missing %s", k)
		}
	}
	if len(Codex.StateRoot()) == 0 {
		t.Fatal("StateRoot must resolve")
	}
}

func TestCodexParseStreamAndFinalize(t *testing.T) {
	events := parse(Codex, codexFixture)
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
	if len(progress) != 2 || progress[0] != "Let me check the current state." || progress[1] != "Done: created report.md with the summary." {
		t.Fatalf("progress = %v", progress)
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "command: base64") {
		t.Fatalf("commands = %v", commands)
	}
	sum, u, err := Codex.Finalize(final)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if sum != "Done: created report.md with the summary." {
		t.Fatalf("summary = %q", sum)
	}
	if u.TokensIn != 2317 || u.TokensOut != 512 {
		t.Fatalf("usage = %+v", u)
	}
	if u.HasCost || u.CostUSD != 0 {
		t.Fatalf("codex must have no cost: %+v", u)
	}
}

func TestCodexParseStreamCommandFailure(t *testing.T) {
	fix := `{"type":"item.completed","item":{"type":"command_execution","command":"make build","status":"completed","exit_code":2,"aggregated_output":"error: boom"}}
{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`
	var errs []string
	for _, e := range parse(Codex, fix) {
		if e.Kind == EventError {
			errs = append(errs, e.Detail)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "exit 2") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestCodexParseStreamNoise(t *testing.T) {
	fix := `not json
{"type":"thread.started"}
{"type":"item.completed","item":{"type":"util_notes","notes":"…"}}
{"type":"turn.completed","usage":{"input_tokens":4,"output_tokens":1}}`
	events := parse(Codex, fix)
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

func TestCodexVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeStub(t, dir, "codex", codexVersionStub)
	v, err := Codex.Version(context.Background(), path)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "0.147.0" {
		t.Fatalf("version = %q", v)
	}
}

const codexVersionStub = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "codex-cli 0.147.0"; exit 0; fi
exit 1`
