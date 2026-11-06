package clirun

import (
	"fmt"
	"strings"
	"testing"
)

func TestToolCallNormalizesInput(t *testing.T) {
	a := ToolCall("Bash", map[string]any{"command": "go test", "description": "x"})
	b := ToolCall("Bash", map[string]any{"description": "x", "command": "go test"})
	if a != b {
		t.Fatalf("key order changed identity: %q vs %q", a, b)
	}
	if ToolCall("command", "go  test\n./...") != ToolCall("command", "go test ./...") {
		t.Fatal("whitespace changed identity")
	}
	if ToolCall("Read", map[string]any{"file_path": "/a"}) == ToolCall("Read", map[string]any{"file_path": "/b"}) {
		t.Fatal("different inputs collided")
	}
}

func TestLoopGuardConsecutive(t *testing.T) {
	var g LoopGuard
	for i := 1; i < LoopRun; i++ {
		if why := g.Observe("x"); why != "" {
			t.Fatalf("stopped at %d: %s", i, why)
		}
	}
	if why := g.Observe("x"); !strings.Contains(why, "8 times in a row") {
		t.Fatalf("reason = %q", why)
	}
}

func TestLoopGuardStreakResetsAndIgnoresUnnamedCalls(t *testing.T) {
	var g LoopGuard
	for i := 0; i < 40; i++ {
		if why := g.Observe(""); why != "" {
			t.Fatal(why)
		}
	}
	for i := 0; i < 20; i++ {
		if why := g.Observe([]string{"a", "b", "c"}[i%3]); why != "" {
			t.Fatalf("rotating calls flagged: %s", why)
		}
	}
}

// A loop that alternates with a harmless call is caught by the window.
func TestLoopGuardWindow(t *testing.T) {
	var g LoopGuard
	var why string
	for i := 0; i < 30 && why == ""; i++ {
		why = g.Observe("loop")
		if why == "" {
			why = g.Observe("other")
		}
	}
	if !strings.Contains(why, "in its last") {
		t.Fatalf("reason = %q", why)
	}
}

// An edit-then-test loop repeats the test call but never the edit, so
// it is work, not a loop.
func TestLoopGuardEditTestIsNotALoop(t *testing.T) {
	var g LoopGuard
	for i := 0; i < 60; i++ {
		if why := g.Observe(fmt.Sprintf("Edit %d", i)); why != "" {
			t.Fatalf("edit %d: %q", i, why)
		}
		if why := g.Observe("Bash go test ./..."); why != "" {
			t.Fatalf("test %d: %q", i, why)
		}
	}
}

func TestClaudeToolUseCarriesFullInputIdentity(t *testing.T) {
	line := `{"type":"assistant","message":{"id":"m","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a.go"}}]}}` + "\n"
	var call string
	var spend Spend
	for ev := range claudeParseStream(strings.NewReader(line)) {
		switch ev.Kind {
		case EventCommand:
			call = ev.Call
		case EventUsage:
			spend = ev.Spend
		}
	}
	if !strings.Contains(call, "/a.go") {
		t.Fatalf("call = %q", call)
	}
	if spend.Tokens != 15 {
		t.Fatalf("spend = %+v", spend)
	}
}

func TestGuardStopIsNotRetried(t *testing.T) {
	f := Classify(false, GuardPrefix+"the same tool call 8 times in a row: Bash")
	if f != FailGuard || f.Retryable() {
		t.Fatalf("class %s retryable %v", f, f.Retryable())
	}
}

func TestOpencodeStepFinishStreamsSpend(t *testing.T) {
	line := `{"type":"step_finish","part":{"reason":"tool-calls","tokens":{"input":1000,"output":200},"cost":0.5}}` + "\n"
	for ev := range OpenCode.ParseStream(strings.NewReader(line)) {
		if ev.Kind == EventUsage && ev.Spend.Tokens == 1200 && ev.Spend.CostUSD == 0.5 {
			return
		}
	}
	t.Fatal("no usage event with the step's spend")
}
