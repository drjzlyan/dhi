package clirun

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func parse(cli *CLI, input string) []StreamEvent {
	ch := cli.ParseStream(strings.NewReader(input))
	var out []StreamEvent
	for e := range ch {
		out = append(out, e)
	}
	return out
}

func TestClaudeBuildArgsGolden(t *testing.T) {
	got := Claude.BuildArgs(RunInput{
		Prompt:  "p",
		System:  "sys",
		Model:   "claude-sonnet-4-5",
		Workdir: "/w",
	})
	want := []string{
		"-p", "p",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
		"--max-turns", "50",
		"--append-system-prompt", "sys",
		"--model", "claude-sonnet-4-5",
		"--add-dir", "/w",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs = %q want %q", got, want)
	}

	got = Claude.BuildArgs(RunInput{Prompt: "p"})
	for _, drop := range []string{"--append-system-prompt", "--model", "--add-dir"} {
		for _, a := range got {
			if a == drop {
				t.Fatalf("flag %q must be dropped when its input is empty", drop)
			}
		}
	}
	if !strings.Contains(strings.Join(got, " "), "stream-json") {
		t.Fatalf("BuildArgs missing stream-json: %v", got)
	}
}

const successStream = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"content":[{"type":"text","text":"Working on it"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","id":"t1","input":{"command":"ls -la"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"file1"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"boom"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Done: two files.","total_cost_usd":0.0123,"usage":{"input_tokens":100,"output_tokens":50}}
`

func TestClaudeParseStream(t *testing.T) {
	events := parse(Claude, successStream)
	var got []string
	var finals []string
	for _, e := range events {
		if e.Kind == EventFinal {
			finals = append(finals, e.Detail)
			continue
		}
		if e.Kind == EventProgress || e.Kind == EventCommand || e.Kind == EventError {
			got = append(got, e.Kind+":"+e.Detail)
		}
	}
	want := []string{
		"progress:Working on it",
		"command:bash: ls -la",
		"error:boom",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %q want %q", got, want)
	}
	if len(finals) != 1 || !strings.Contains(finals[0], "Done: two files.") {
		t.Fatalf("final events = %d, first=%q", len(finals), firstOrEmpty(finals))
	}
}

func TestClaudeParseStreamMalformedLineTolerated(t *testing.T) {
	events := parse(Claude, "this is not json\n"+successStream)
	errored, final := false, false
	for _, e := range events {
		if e.Kind == EventError && strings.Contains(e.Detail, "malformed") {
			errored = true
		}
		if e.Kind == EventFinal {
			final = true
		}
	}
	if !errored {
		t.Fatal("malformed line must produce an error event but the stream must continue")
	}
	if !final {
		t.Fatal("stream must continue to the final event after a malformed line")
	}
}

func TestClaudeFinalize(t *testing.T) {
	sum, u, err := Claude.Finalize(`{"type":"result","subtype":"success","is_error":false,"result":"Done.","total_cost_usd":0.0123,"usage":{"input_tokens":100,"output_tokens":50}}`)
	if err != nil {
		t.Fatalf("finalize success: %v", err)
	}
	if sum != "Done." {
		t.Errorf("summary = %q", sum)
	}
	if u.TokensIn != 100 || u.TokensOut != 50 || u.CostUSD != 0.0123 || !u.HasCost {
		t.Errorf("usage = %+v", u)
	}

	if _, _, err := Claude.Finalize(`{"type":"result","subtype":"error_max_turns","is_error":true,"result":""}`); err == nil {
		t.Fatal("error subtype must fail")
	}
	if _, _, err := Claude.Finalize(`not json`); err == nil {
		t.Fatal("malformed final must fail")
	}
}

func TestClaudeE2E(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "claude", claudeStub)
	r := NewRegistry(stubLook(dir))
	cli, _ := r.Get("claude")

	path, err := stubLook(dir)("claude")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path, cli.BuildArgs(RunInput{
		Prompt: "summarize", System: "sys",
	})...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	var final string
	for e := range cli.ParseStream(stdout) {
		if e.Kind == EventFinal {
			final = e.Detail
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if final == "" {
		t.Fatal("no final event from real spawn")
	}
	sum, u, err := cli.Finalize(final)
	if err != nil {
		t.Fatalf("finalize after spawn: %v", err)
	}
	if !strings.Contains(sum, "Done: two files.") {
		t.Errorf("summary = %q", sum)
	}
	if u.TokensIn != 100 || u.CostUSD != 0.0123 {
		t.Errorf("usage = %+v", u)
	}
}

func firstOrEmpty(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}
