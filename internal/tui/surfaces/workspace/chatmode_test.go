package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
)

type modeRecorder struct {
	stubTurner
	calls chan [3]string
}

func (m *modeRecorder) ReadOnlyTurn(_ context.Context, agent string, trig bus.Message, mode, req string) error {
	m.calls <- [3]string{agent, mode, req + "|" + trig.Text}
	return nil
}

func TestParseModeCmd(t *testing.T) {
	cases := map[string][2]string{
		"/plan add a flag":  {"plan", "add a flag"},
		" /REVIEW the diff": {"review", "the diff"},
		"/ask why":          {"ask", "why"},
		"/plan":             {"plan", ""},
	}
	for in, want := range cases {
		mode, req, ok := parseModeCmd(in)
		if !ok || mode != want[0] || req != want[1] {
			t.Errorf("%q = %q,%q,%v", in, mode, req, ok)
		}
	}
	for _, in := range []string{"/planet x", "plan x", "/model x"} {
		if _, _, ok := parseModeCmd(in); ok {
			t.Errorf("%q must not be a mode command", in)
		}
	}
}

func TestPlanCommandStartsAReadOnlyTurn(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	rec := &modeRecorder{calls: make(chan [3]string, 1)}
	p.rt = rec
	p.channels = []string{"#general", "dm:alice"}
	p.active = 1
	if !p.modeCommand("/plan add a flag") || p.flash != "" || !strings.Contains(p.note, "planning") {
		t.Fatalf("flash=%q note=%q", p.flash, p.note)
	}
	select {
	case got := <-rec.calls:
		if got != [3]string{"alice", "plan", "add a flag|/plan add a flag"} {
			t.Errorf("call = %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the runtime was not asked to plan")
	}
	if h := p.bus.History("dm:alice", 0); len(h) != 1 || h[0].Text != "/plan add a flag" {
		t.Fatalf("the request must be posted once, without a normal turn: %v", h)
	}
}

func TestModeCommandRefusals(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.rt = &modeRecorder{calls: make(chan [3]string, 1)}
	p.channels = []string{"#general", "dm:alice"}
	p.active = 0
	if !p.modeCommand("/plan x") || !strings.Contains(p.flash, "direct message") {
		t.Errorf("flash = %q", p.flash)
	}
	p.active = 1
	if !p.modeCommand("/ask") || !strings.Contains(p.flash, "needs text") {
		t.Errorf("flash = %q", p.flash)
	}
	p.rt = &stubTurner{}
	if !p.modeCommand("/ask why") || !strings.Contains(p.flash, "crew running") {
		t.Errorf("flash = %q", p.flash)
	}
}
