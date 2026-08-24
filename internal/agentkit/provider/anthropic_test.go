package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const sseText = `event: message_start
data: {"type":"message_start"}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

data: {"type":"message_stop"}
`

const sseTool = `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"api:main.go\"}"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

data: {"type":"message_stop"}
`

const sseReply = `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"It starts with "}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"package main."}}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

data: {"type":"message_stop"}
`

// fixture serves canned SSE bodies in sequence and records requests.
type fixture struct {
	mu        sync.Mutex
	responses []string
	status    int // when non-zero, respond with this code instead
	bodies    [][]byte
	headers   []http.Header
	srv       *httptest.Server
}

func newFixture(t *testing.T, responses ...string) *fixture {
	t.Helper()
	f := &fixture{responses: responses}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.headers = append(f.headers, r.Header.Clone())
		i := len(f.bodies) - 1
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			http.Error(w, "nope", status)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, f.responses[i])
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) provider(t *testing.T) (*Anthropic, func() []Request) {
	t.Helper()
	p := NewAnthropic(f.srv.URL, "test-key")
	return p, f.recorded
}

// recorded decodes every captured wire body back into neutral Requests,
// verifying the adapter survives its own serialization round trip.
func (f *fixture) recorded() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, 0, len(f.bodies))
	for _, b := range f.bodies {
		var wr wireRequest
		if json.Unmarshal(b, &wr) != nil {
			continue
		}
		req := Request{Model: wr.Model, System: wr.System}
		for _, wm := range wr.Messages {
			m := Message{Role: wm.Role}
			for _, wb := range wm.Content {
				switch wb.Type {
				case "text":
					m.Blocks = append(m.Blocks, Text{Value: wb.Text})
				case "tool_use":
					m.Blocks = append(m.Blocks, ToolUse{ID: wb.ID, Name: wb.Name, Input: wb.Input})
				case "tool_result":
					m.Blocks = append(m.Blocks, ToolResult{ToolUseID: wb.ToolUseID, Content: wb.Text, IsError: wb.IsError})
				}
			}
			req.Messages = append(req.Messages, m)
		}
		out = append(out, req)
	}
	return out
}

func TestAnthropicConformance(t *testing.T) {
	mk := func(t *testing.T, sc scenario) (Provider, func() []Request) {
		t.Helper()
		var f *fixture
		switch sc {
		case scenText:
			f = newFixture(t, sseText)
		case scenRoundTrip:
			f = newFixture(t, sseTool, sseReply)
		case scenCancel:
			f = newFixture(t, sseText)
		}
		return f.provider(t)
	}
	runSuite(t, mk)
}

func TestAnthropicWireFormat(t *testing.T) {
	f := newFixture(t, sseText)
	p := NewAnthropic(f.srv.URL, "sk-test")
	ch, err := p.Stream(context.Background(), Request{
		Model:     "claude-sonnet-4-5",
		System:    "be brief",
		MaxTokens: 777,
		Messages:  []Message{{Role: User, Blocks: []Block{Text{Value: "hi"}}}},
		Tools:     []ToolDef{{Name: "read", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collect(t, ch)

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 {
		t.Fatalf("%d requests, want 1", len(f.bodies))
	}
	var wr wireRequest
	if err := json.Unmarshal(f.bodies[0], &wr); err != nil {
		t.Fatalf("decode request: %v (%s)", err, f.bodies[0])
	}
	if wr.Model != "claude-sonnet-4-5" || wr.MaxTokens != 777 || !wr.Stream {
		t.Errorf("wire request wrong: %+v", wr)
	}
	if wr.System != "be brief" {
		t.Errorf("System = %q", wr.System)
	}
	if len(wr.Messages) != 1 || wr.Messages[0].Role != User || wr.Messages[0].Content[0].Text != "hi" {
		t.Errorf("messages wrong: %+v", wr.Messages)
	}
	if len(wr.Tools) != 1 || wr.Tools[0].Name != "read" {
		t.Errorf("tools wrong: %+v", wr.Tools)
	}
	h := f.headers[0]
	if h.Get("x-api-key") != "sk-test" {
		t.Errorf("x-api-key = %q", h.Get("x-api-key"))
	}
	if h.Get("anthropic-version") != APIVersion {
		t.Errorf("anthropic-version = %q", h.Get("anthropic-version"))
	}
}

func TestAnthropicAuthError(t *testing.T) {
	f := newFixture(t, "{}")
	f.status = http.StatusUnauthorized
	p := NewAnthropic(f.srv.URL, "bad-key")
	_, err := p.Stream(context.Background(), Request{Model: "m"})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not mention status", err)
	}
}

func TestAnthropicStreamErrorEvent(t *testing.T) {
	f := newFixture(t, `data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}

`)
	p := NewAnthropic(f.srv.URL, "k")
	ch, err := p.Stream(context.Background(), Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evs := collect(t, ch)
	if len(evs) != 1 || evs[0].Kind != EventError {
		t.Fatalf("evs = %+v, want one error event", evs)
	}
	if !strings.Contains(evs[0].Err.Error(), "overloaded_error") {
		t.Errorf("err = %v", evs[0].Err)
	}
}

func TestAnthropicTruncatedStream(t *testing.T) {
	// Server hangs up without message_stop: consumer still gets a
	// terminal stop so nothing waits forever.
	f := newFixture(t, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}
`)
	p := NewAnthropic(f.srv.URL, "k")
	ch, err := p.Stream(context.Background(), Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evs := collect(t, ch)
	last := evs[len(evs)-1]
	if last.Kind != EventStop {
		t.Errorf("last event = %+v, want stop", last)
	}
	if texts(evs) != "partial" {
		t.Errorf("text = %q", texts(evs))
	}
}
