package workspace

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestParseModelCmd(t *testing.T) {
	for in, want := range map[string]struct {
		arg string
		ok  bool
	}{
		"/model":         {"", true},
		" /MODEL  opus ": {"opus", true},
		"/model clear":   {"clear", true},
		"/model a b":     {"", false},
		"/models":        {"", false},
		"hello /model":   {"", false},
		"":               {"", false},
	} {
		if arg, ok := parseModelCmd(in); arg != want.arg || ok != want.ok {
			t.Errorf("%q = %q,%v want %q,%v", in, arg, ok, want.arg, want.ok)
		}
	}
}

func TestModelCommandSetsShowsClears(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	p, ws, _, _ := newPaneFixture(t)
	st := episode.Open(ws.Root)
	if err := st.Put(episode.Episode{Key: "thread:dm:alice:7", Agent: "alice", Turns: 1, LastContext: 6000, Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	p.meter = &chatMeter{episodes: st, limits: func() (int, int) { return 24000, 160000 }}
	p.channels = []string{"#general", "dm:alice"}

	// channels refuse by name
	p.active = 0
	if !p.modelCommand("/model opus") || !strings.Contains(p.flash, "direct message") {
		t.Fatalf("channel: flash=%q", p.flash)
	}

	p.active, p.threadID = 1, 0
	if !p.modelCommand("/model opus-x") {
		t.Fatal("not handled")
	}
	ep, _, _ := st.Get("alice", "thread:dm:alice:7")
	if ep.Model != "opus-x" {
		t.Fatalf("not persisted: %+v", ep)
	}
	h := ansi.Strip(p.headerMeter(p.header(), 120))
	if !strings.Contains(h, "model opus-x") || !strings.Contains(h, "ctx 6k/24k 25%") {
		t.Errorf("header keeps meter and shows model: %q", h)
	}
	p.modelCommand("/model")
	if !strings.Contains(p.note, "opus-x") {
		t.Errorf("show: %q", p.note)
	}
	p.modelCommand("/model -rf")
	if !strings.Contains(p.flash, "'-'") {
		t.Errorf("dash: %q", p.flash)
	}
	p.modelCommand("/model clear")
	ep, _, _ = st.Get("alice", "thread:dm:alice:7")
	if ep.Model != "" {
		t.Fatalf("not cleared: %+v", ep)
	}
	if h := ansi.Strip(p.headerMeter(p.header(), 120)); strings.Contains(h, "model ") {
		t.Errorf("chip should be gone: %q", h)
	}
}

func TestModelCommandNeedsConversation(t *testing.T) {
	p, ws, _, _ := newPaneFixture(t)
	p.meter = &chatMeter{episodes: episode.Open(ws.Root)}
	p.channels = []string{"#general", "dm:alice"}
	p.active = 1
	if !p.modelCommand("/model opus") || !strings.Contains(p.flash, "no conversation yet") {
		t.Errorf("flash=%q", p.flash)
	}
}
