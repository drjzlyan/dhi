package clirun

import (
	"strings"
	"testing"
)

// F-086 (live): each assistant message reports its own prompt size; the
// runtime keeps the last one as the session's real context.
func TestClaudeAssistantUsageIsOneCallsContext(t *testing.T) {
	stream := `{"type":"assistant","message":{"usage":{"input_tokens":3,"cache_read_input_tokens":12000,"cache_creation_input_tokens":500},"content":[{"type":"text","text":"hi"}]}}
{"type":"assistant","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":13000,"cache_creation_input_tokens":0},"content":[{"type":"text","text":"done"}]}}
`
	last := 0
	for ev := range claudeParseStream(strings.NewReader(stream)) {
		if ev.Kind == EventUsage {
			last = ev.Context
		}
	}
	if last != 13005 {
		t.Fatalf("last context = %d, want 13005", last)
	}
}

func TestClaudeInitCarriesTheSession(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"abc-123"}
`
	for ev := range claudeParseStream(strings.NewReader(stream)) {
		if ev.Kind == EventSession && ev.Session == "abc-123" {
			return
		}
	}
	t.Fatal("no session event from the init line")
}

// Live (M32): opencode tool failures named only the tool; the reason
// (and a sandbox denial in it) must come through.
func TestOpencodeToolErrorCarriesTheReason(t *testing.T) {
	line := `{"type":"tool_use","part":{"tool":"bash","state":{"status":"error","input":{"command":"touch /opt/x/y"},"error":"touch: /opt/x/y: Operation not permitted"}}}` + "\n"
	for ev := range OpenCode.ParseStream(strings.NewReader(line)) {
		if ev.Kind == EventError {
			if !strings.Contains(ev.Detail, "Operation not permitted") || ev.Denied != "/opt/x" {
				t.Fatalf("event = %+v", ev)
			}
			return
		}
	}
	t.Fatal("no error event")
}

// Live (M32): cursor-agent 2026.09.26 nests tool calls; lines read
// "tool: " with nothing after.
func TestCursorNestedToolCalls(t *testing.T) {
	stream := `{"type":"tool_call","subtype":"started","tool_call":{"shellToolCall":{"args":{"command":"go test ./..."}}}}
{"type":"tool_call","subtype":"completed","tool_call":{"shellToolCall":{"args":{"command":"touch /opt/x/y"},"result":{"failure":{"exitCode":1,"stderr":"touch: /opt/x/y: Operation not permitted\n"}}}}}
{"type":"tool_call","subtype":"completed","tool_call":{"readToolCall":{"args":{"path":"/a"},"result":{"error":{"errorMessage":"File not found"}}}}}
`
	var got []StreamEvent
	for ev := range CursorAgent.ParseStream(strings.NewReader(stream)) {
		got = append(got, ev)
	}
	if len(got) != 3 || got[0].Detail != "tool shell: go test ./..." ||
		!strings.Contains(got[1].Detail, "Operation not permitted") || got[1].Denied != "/opt/x" ||
		!strings.Contains(got[2].Detail, "File not found") {
		t.Fatalf("events = %+v", got)
	}
}

// Live (M32): antigravity text deltas became one chat line each.
func TestAntigravityCoalescesTextDeltas(t *testing.T) {
	stream := `{"event":"step_update","step_update":{"step_type":"agent_response","state":"ACTIVE","text_delta":"The task is ready for re"}}
{"event":"step_update","step_update":{"step_type":"agent_response","state":"ACTIVE","text_delta":"view."}}
{"event":"step_update","step_update":{"step_type":"agent_response","state":"ACTIVE","text_delta":" Next I"}}
{"event":"step_update","step_update":{"step_type":"tool","state":"ACTIVE","tool_name":"view_file"}}
`
	var got []string
	for ev := range Antigravity.ParseStream(strings.NewReader(stream)) {
		got = append(got, ev.Kind+":"+ev.Detail)
	}
	want := []string{"progress:The task is ready for review.", "progress:Next I", "command:tool: view_file"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %q", got)
	}
}
