package provider

import (
	"context"
	"sync"
)

// Mock is a scripted Provider (ADR-0003): each Stream call consumes the
// next script and emits its events in order. Scripts make every agent
// test deterministic, offline, and fast. Requests are recorded for
// assertions.
type Mock struct {
	mu      sync.Mutex
	scripts [][]Event
	calls   []Request
	n       int
}

// NewMock returns a provider playing scripts in call order.
func NewMock(scripts ...[]Event) *Mock {
	return &Mock{scripts: scripts}
}

// Add appends scripts for subsequent Stream calls (tests build scenarios
// incrementally).
func (m *Mock) Add(scripts ...[]Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts = append(m.scripts, scripts...)
}

// ScriptText builds a script emitting text deltas then stopping cleanly.
func ScriptText(parts ...string) []Event {
	evs := make([]Event, 0, len(parts)+1)
	for _, p := range parts {
		evs = append(evs, Event{Kind: EventText, Text: p})
	}
	return append(evs, Event{Kind: EventStop, StopReason: StopEndTurn})
}

// ScriptToolCall builds a script emitting one complete tool call then stopping
// with a tool_use reason (the runtime answers with a ToolResult turn).
func ScriptToolCall(id, name string, input []byte) []Event {
	return []Event{
		{Kind: EventToolUse, ToolUseID: id, ToolName: name, Input: input},
		{Kind: EventStop, StopReason: StopToolUse},
	}
}

// Stream plays the next script. Calls beyond the script list fail the
// stream immediately — tests should never hit that silently.
func (m *Mock) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.n >= len(m.scripts) {
		ch := make(chan Event, 1)
		ch <- Event{Kind: EventError, Err: errf("mock: no script for call %d", m.n+1)}
		close(ch)
		m.n++
		return ch, nil
	}
	script := m.scripts[m.n]
	m.n++
	out := make(chan Event, len(script))
	go func() {
		defer close(out)
		for _, ev := range script {
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Calls returns copies of recorded requests in order.
func (m *Mock) Calls() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, len(m.calls))
	copy(out, m.calls)
	return out
}
