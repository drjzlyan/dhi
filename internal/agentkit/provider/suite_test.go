package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// collect drains an event stream, failing the test if it does not close
// within a generous watchdog (deadlock protection, not timing assertion).
func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var out []Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("stream did not close; got %d events", len(out))
		}
	}
}

func texts(evs []Event) string {
	s := ""
	for _, ev := range evs {
		if ev.Kind == EventText {
			s += ev.Text
		}
	}
	return s
}

// scenario names the conversation shape a subtest needs, so mk can
// prepare a fresh provider whose scripts line up call-for-call.
type scenario string

const (
	scenText      scenario = "text"
	scenRoundTrip scenario = "roundtrip"
	scenCancel    scenario = "cancel"
)

// runSuite is the conformance contract every Provider must satisfy
// (ADR-0003): identical semantics regardless of backend.
func runSuite(t *testing.T, mk func(t *testing.T, sc scenario) (Provider, func() []Request)) {
	t.Helper()

	t.Run("text ordering", func(t *testing.T) {
		p, _ := mk(t, scenText)
		ch, err := p.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: User, Blocks: []Block{Text{Value: "hi"}}}}})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		evs := collect(t, ch)
		if texts(evs) != "Hello world" {
			t.Errorf("assembled text = %q", texts(evs))
		}
		if n := len(evs); n == 0 || evs[n-1].Kind != EventStop || evs[n-1].StopReason != StopEndTurn {
			t.Errorf("missing terminal stop/end_turn: %+v", evs)
		}
	})

	t.Run("tool round trip", func(t *testing.T) {
		p, calls := mk(t, scenRoundTrip)
		ctx := context.Background()
		ch, err := p.Stream(ctx, Request{Model: "m", Messages: []Message{{Role: User, Blocks: []Block{Text{Value: "read main"}}}}})
		if err != nil {
			t.Fatalf("Stream 1: %v", err)
		}
		evs := collect(t, ch)
		var use *Event
		for i := range evs {
			if evs[i].Kind == EventToolUse {
				use = &evs[i]
			}
		}
		if use == nil {
			t.Fatalf("no tool_use event: %+v", evs)
		}
		if use.ToolName != "read" || use.ToolUseID == "" {
			t.Errorf("tool_use = %+v", use)
		}
		var input struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(use.Input, &input); err != nil {
			t.Fatalf("tool input not valid JSON: %v (%s)", err, use.Input)
		}
		if input.Path != "api:main.go" {
			t.Errorf("input.path = %q", input.Path)
		}

		// Feed the result back; expect a plain text completion.
		ch, err = p.Stream(ctx, Request{
			Model: "m",
			Messages: []Message{
				{Role: User, Blocks: []Block{Text{Value: "read main"}}},
				{Role: Assistant, Blocks: []Block{ToolUse{ID: use.ToolUseID, Name: use.ToolName, Input: use.Input}}},
				{Role: User, Blocks: []Block{ToolResult{ToolUseID: use.ToolUseID, Content: "package main"}}},
			},
		})
		if err != nil {
			t.Fatalf("Stream 2: %v", err)
		}
		evs = collect(t, ch)
		if texts(evs) != "It starts with package main." {
			t.Errorf("second response = %q", texts(evs))
		}

		rec := calls()
		if len(rec) != 2 {
			t.Fatalf("recorded %d requests, want 2", len(rec))
		}
		last := rec[1]
		if len(last.Messages) != 3 {
			t.Fatalf("second request has %d messages, want 3", len(last.Messages))
		}
		res, ok := last.Messages[2].Blocks[0].(ToolResult)
		if !ok || res.ToolUseID != use.ToolUseID || res.Content != "package main" {
			t.Errorf("tool_result block wrong: %#v", last.Messages[2].Blocks[0])
		}
	})

	t.Run("cancel mid-stream", func(t *testing.T) {
		p, _ := mk(t, scenCancel)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		ch, err := p.Stream(ctx, Request{Model: "m"})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		got := 0
		loop := true
		for loop {
			select {
			case _, ok := <-ch:
				if !ok {
					loop = false
					continue
				}
				got++
				cancel()
			case <-time.After(5 * time.Second):
				t.Fatal("stream hung after cancel")
			}
		}
		if ctx.Err() == nil {
			t.Error("context not cancelled")
		}
		_ = got
	})
}

func TestMockConformance(t *testing.T) {
	mk := func(t *testing.T, sc scenario) (Provider, func() []Request) {
		t.Helper()
		var m *Mock
		switch sc {
		case scenText:
			m = NewMock(ScriptText("Hello ", "world"))
		case scenRoundTrip:
			m = NewMock(
				ScriptToolCall("toolu_mock1", "read", []byte(`{"path":"api:main.go"}`)),
				ScriptText("It starts with package main."),
			)
		case scenCancel:
			m = NewMock(ScriptText("a", "b", "c"))
		}
		return m, m.Calls
	}
	runSuite(t, mk)
}

func TestMockExhausted(t *testing.T) {
	m := NewMock(ScriptText("only"))
	ch, err := m.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("first Stream: %v", err)
	}
	collect(t, ch)
	ch, err = m.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("second Stream: %v", err)
	}
	evs := collect(t, ch)
	if len(evs) != 1 || evs[0].Kind != EventError {
		t.Errorf("evs = %+v, want single error", evs)
	}
}
