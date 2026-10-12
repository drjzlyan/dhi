package clirun

import (
	"slices"
	"strings"
	"testing"
)

// F-066: resume and max-turns reach the argv of the verified adapters.
func TestClaudeResumeAndMaxTurnsArgs(t *testing.T) {
	args := Claude.BuildArgs(RunInput{Prompt: "go", Resume: "sess-1", MaxTurns: 12})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--resume sess-1") || !strings.Contains(joined, "--max-turns 12") {
		t.Fatalf("args = %v", args)
	}
	fresh := strings.Join(Claude.BuildArgs(RunInput{Prompt: "go"}), " ")
	if strings.Contains(fresh, "--resume") || !strings.Contains(fresh, "--max-turns 50") {
		t.Fatalf("fresh args = %s", fresh)
	}
	stdin := strings.Join(Claude.BuildArgs(RunInput{Stdin: "x", Resume: "sess-2"}), " ")
	if !strings.Contains(stdin, "--resume sess-2") {
		t.Fatalf("stdin delivery lost resume: %s", stdin)
	}
}

func TestClaudeFinalizeSessionID(t *testing.T) {
	_, u, err := Claude.Finalize(`{"type":"result","subtype":"success","result":"ok","session_id":"d3b2","usage":{"input_tokens":1,"output_tokens":1}}`)
	if err != nil || u.SessionID != "d3b2" {
		t.Fatalf("SessionID = %q, %v", u.SessionID, err)
	}
}

func TestCodexResumeArgs(t *testing.T) {
	args := Codex.BuildArgs(RunInput{Prompt: "go", Workdir: "/w", Resume: "th-9", MCPURL: "http://127.0.0.1:1/mcp"})
	if args[0] != "exec" || args[1] != "resume" {
		t.Fatalf("args = %v", args)
	}
	if slices.Contains(args, "-C") || slices.Contains(args, "--sandbox") {
		t.Fatalf("exec resume takes no -C/--sandbox: %v", args)
	}
	if i := slices.Index(args, "th-9"); i < 0 || i != len(args)-2 || args[len(args)-1] != "go" {
		t.Fatalf("session id must precede the prompt: %v", args)
	}
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("bypass flag missing: %v", args)
	}
}

func TestCodexThreadIDBecomesSession(t *testing.T) {
	stream := `{"type":"thread.started","thread_id":"01a1"}
{"type":"item.completed","item":{"id":"i","type":"agent_message","text":"mango"}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":1}}`
	var final string
	for ev := range Codex.ParseStream(strings.NewReader(stream)) {
		if ev.Kind == EventFinal {
			final = ev.Detail
		}
	}
	_, u, err := Codex.Finalize(final)
	if err != nil || u.SessionID != "01a1" || u.TokensIn != 6 {
		t.Fatalf("usage = %+v, %v", u, err)
	}
}

// Only verified adapters may claim resume.
func TestResumeOKOnlyVerified(t *testing.T) {
	for _, c := range allAdapters() {
		want := c.Name == "claude" || c.Name == "codex"
		if c.ResumeOK != want {
			t.Fatalf("%s.ResumeOK = %v, want %v", c.Name, c.ResumeOK, want)
		}
	}
}
