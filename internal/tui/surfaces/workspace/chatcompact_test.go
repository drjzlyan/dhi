package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
)

// compactRecorder is a runtime that also compacts (F-135).
type compactRecorder struct {
	stubTurner
	calls chan [3]any
}

func (c *compactRecorder) Compact(_ context.Context, agent, ch string, thread int64) error {
	c.calls <- [3]any{agent, ch, thread}
	return nil
}

func TestParseCompactCmd(t *testing.T) {
	for in, ok := range map[string]bool{"/compact": true, " /COMPACT ": true, "/compact now": false, "/compacts": false} {
		cmd, _, got := parseConvCmd(in)
		if got != ok || (ok && cmd != "compact") {
			t.Errorf("%q = %q,%v want ok=%v", in, cmd, got, ok)
		}
	}
}

func TestCompactCommandAsksTheRuntime(t *testing.T) {
	p, ws, _, _ := newPaneFixture(t)
	rec := &compactRecorder{calls: make(chan [3]any, 1)}
	p.rt = rec
	st := episode.Open(ws.Root)
	root, _ := p.bus.Post(bus.Message{Channel: "dm:alice", Author: bus.Human, Text: "hi"})
	if err := st.Put(episode.Episode{Key: episode.Key("", "dm:alice", root.ID), Agent: "alice", Turns: 1}); err != nil {
		t.Fatal(err)
	}
	p.meter = &chatMeter{episodes: st}
	p.channels = []string{"#general", "dm:alice"}
	p.active, p.threadID = 1, root.ID

	if !p.conversationCommand("/compact") || p.flash != "" || !strings.Contains(p.note, "compacting") {
		t.Fatalf("flash=%q note=%q", p.flash, p.note)
	}
	select {
	case got := <-rec.calls:
		if got != [3]any{"alice", "dm:alice", root.ID} {
			t.Errorf("compact call = %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the runtime was not asked to compact")
	}
}

func TestCompactCommandRefusals(t *testing.T) {
	p, ws, _, _ := newPaneFixture(t)
	p.meter = &chatMeter{episodes: episode.Open(ws.Root)}
	p.channels = []string{"#general", "dm:alice"}

	p.active = 0
	if !p.conversationCommand("/compact") || !strings.Contains(p.flash, "direct message") {
		t.Errorf("channel: %q", p.flash)
	}
	p.active = 1
	p.conversationCommand("/compact") // a plain turn handler cannot compact
	if !strings.Contains(p.flash, "crew running") {
		t.Errorf("no runtime: %q", p.flash)
	}
	p.rt = &compactRecorder{calls: make(chan [3]any, 1)}
	p.conversationCommand("/compact")
	if !strings.Contains(p.flash, "open the conversation") {
		t.Errorf("no thread: %q", p.flash)
	}
	p.threadID = 9
	p.conversationCommand("/compact")
	if !strings.Contains(p.flash, "no conversation to compact yet") {
		t.Errorf("no episode: %q", p.flash)
	}
}
