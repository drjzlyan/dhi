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
