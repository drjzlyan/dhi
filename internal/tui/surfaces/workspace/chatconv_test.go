package workspace

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
)

func TestParseConvCmd(t *testing.T) {
	for in, want := range map[string]struct {
		cmd, rest string
		ok        bool
	}{
		"/fork":          {"fork", "", true},
		" /FORK ":        {"fork", "", true},
		"/fork now":      {"", "", false},
		"/new":           {"new", "", true},
		"/new try again": {"new", "try again", true},
		"/news":          {"", "", false},
		"hello /new":     {"", "", false},
		"":               {"", "", false},
	} {
		if cmd, rest, ok := parseConvCmd(in); cmd != want.cmd || rest != want.rest || ok != want.ok {
			t.Errorf("%q = %q,%q,%v want %+v", in, cmd, rest, ok, want)
		}
	}
}

func TestForkCommandStartsThreadWithCopiedEpisode(t *testing.T) {
	p, ws, _, _ := newPaneFixture(t)
	st := episode.Open(ws.Root)
	root, err := p.bus.Post(bus.Message{Channel: "dm:alice", Author: bus.Human, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	srcKey := episode.Key("", "dm:alice", root.ID)
	if err := st.Put(episode.Episode{Key: srcKey, Agent: "alice", Turns: 2, Session: "s1", CLI: "claude",
		LastSummary: "did a", Model: "opus-x", Plan: []episode.PlanStep{{Title: "a", Status: "doing"}},
		Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	p.meter = &chatMeter{episodes: st}
	p.channels = []string{"#general", "dm:alice"}
	p.active, p.threadID = 1, root.ID

	if !p.conversationCommand("/fork") || p.flash != "" {
		t.Fatalf("flash=%q", p.flash)
	}
	if p.threadID == 0 || p.threadID == root.ID {
		t.Fatalf("not moved to the fork: %d", p.threadID)
	}
	f, ok, _ := st.Get("alice", episode.Key("", "dm:alice", p.threadID))
	if !ok || f.Model != "opus-x" || f.LastSummary != "did a" || len(f.Plan) != 1 || f.ForkedFrom != srcKey {
		t.Fatalf("fork episode: %+v", f)
	}
	if f.Session != "" || f.CLI != "" {
		t.Errorf("CLI session must not be shared: %+v", f)
	}
	src, _, _ := st.Get("alice", srcKey)
	if src.Session != "s1" || src.Turns != 2 {
		t.Errorf("original touched: %+v", src)
	}
	if len(p.bus.History("dm:alice", 0)) == 0 {
		t.Error("no fork root posted")
	}
}

func TestForkCommandRefusals(t *testing.T) {
	p, ws, _, _ := newPaneFixture(t)
	p.meter = &chatMeter{episodes: episode.Open(ws.Root)}
	p.channels = []string{"#general", "dm:alice"}

	p.active = 0
	if !p.conversationCommand("/fork") || !strings.Contains(p.flash, "direct message") {
		t.Errorf("channel: %q", p.flash)
	}
	p.active, p.threadID = 1, 0
	p.conversationCommand("/fork")
	if !strings.Contains(p.flash, "open the conversation") {
		t.Errorf("no thread: %q", p.flash)
	}
	p.threadID = 5
	p.conversationCommand("/fork")
	if !strings.Contains(p.flash, "no conversation to fork") {
		t.Errorf("no episode: %q", p.flash)
	}
}

func TestNewCommandLeavesThreadOrPosts(t *testing.T) {
	p, ws, _, turns := newPaneFixture(t)
	p.meter = &chatMeter{episodes: episode.Open(ws.Root)}
	p.channels = []string{"#general", "dm:alice"}
	p.active, p.threadID = 1, 9

	p.conversationCommand("/new")
	if p.threadID != 0 || !strings.Contains(p.note, "new conversation") {
		t.Errorf("thread=%d note=%q", p.threadID, p.note)
	}
	p.threadID = 9
	p.conversationCommand("/new start over")
	if p.threadID != 0 || len(turns.handled) != 1 || turns.handled[0].Thread != 0 || turns.handled[0].Text != "start over" {
		t.Errorf("thread=%d handled=%+v", p.threadID, turns.handled)
	}
	p.active = 0
	p.conversationCommand("/new")
	if !strings.Contains(p.flash, "direct message") {
		t.Errorf("channel: %q", p.flash)
	}
}
