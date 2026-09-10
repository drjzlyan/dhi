package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/autopilot"
)

func autoClock(y, m, d, hh, mm int) time.Time {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC)
}

// stubRoster satisfies profile.Roster for section tests.
type stubRoster struct{ ids []string }

func (s *stubRoster) AgentIDs() []string { return s.ids }

func (s *stubRoster) Manifest(id string) (*manifest.Agent, bool) {
	for _, i := range s.ids {
		if i == id {
			return &manifest.Agent{ID: id, Name: strings.ToUpper(id),
				Model: "mock-1", Runtime: "claude", Tools: []string{"read"}}, true
		}
	}
	return nil, false
}

// autoSurface seeds a workspace with a bus + roster and a frozen clock
// for deterministic due math. The card UI lives in Settings (F-023);
// these tests exercise the execution engine that stays here.
func autoSurface(t *testing.T) (*Model, *autopilot.Store) {
	t.Helper()
	m, ws := newSurface(t)
	m.roster = &stubRoster{ids: []string{"alice", "bob"}}
	m.now = func() time.Time { return autoClock(2026, 9, 2, 9, 0) }
	as, err := autopilot.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.autopilots = as
	if b, err := bus.Open(ws); err == nil {
		m.bus = b
	}
	return m, as
}

func mustKeyboard(t *testing.T, s string) autopilot.Schedule {
	t.Helper()
	sch, err := autopilot.ParseSchedule(s)
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

type capturingTurns struct{ posts chan bus.Message }

func (c *capturingTurns) Handle(_ context.Context, msg bus.Message) {
	select {
	case c.posts <- msg:
	default:
	}
}

func TestAutopilotRunPostsToDm(t *testing.T) {
	m, as := autoSurface(t)
	capT := &capturingTurns{posts: make(chan bus.Message, 4)}
	m.rt = capT
	_, _ = as.Create("standup", "Morning standup", "alice", "summarize", mustKeyboard(t, "daily 09:00"))
	card, _ := as.Get("standup")
	if err := m.autopilotRun(card); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-capT.posts:
		if msg.Channel != "dm:alice" {
			t.Fatalf("channel = %q, want dm:alice", msg.Channel)
		}
		if !strings.HasPrefix(msg.Text, "[autopilot standup]") {
			t.Fatalf("text = %q, want autopilot prefix", msg.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("no turn posted")
	}
}

func TestAutopilotDanglingAgentRefuses(t *testing.T) {
	m, as := autoSurface(t)
	capT := &capturingTurns{posts: make(chan bus.Message, 4)}
	m.rt = capT
	_, _ = as.Create("ghost", "Ghost", "ghost", "p", mustKeyboard(t, "daily 09:00"))
	card, _ := as.Get("ghost")
	if err := m.autopilotRun(card); err == nil ||
		!strings.Contains(err.Error(), "not on roster") {
		t.Fatalf("dangling run err = %v, want named roster fix", err)
	}
	select {
	case msg := <-capT.posts:
		t.Fatalf("dangling agent posted %+v", msg)
	case <-time.After(20 * time.Millisecond):
	}
	c, _ := as.Get("ghost")
	if !c.LastRun.IsZero() {
		t.Fatal("dangling agent marked ran — must not")
	}
}

func TestCatchUpRunsDueInSlugOrderAndOnce(t *testing.T) {
	m, as := autoSurface(t)
	capT := &capturingTurns{posts: make(chan bus.Message, 8)}
	m.rt = capT
	// b due (interval never ran), a due (interval never ran), paused c,
	// dangling ghost due but refuses.
	_, _ = as.Create("a", "A", "alice", "p", mustKeyboard(t, "interval 1h"))
	_, _ = as.Create("b", "B", "bob", "p", mustKeyboard(t, "interval 1h"))
	_, _ = as.Create("c", "C", "alice", "p", mustKeyboard(t, "interval 1h"))
	_, _ = as.Create("dangling", "D", "ghost", "p", mustKeyboard(t, "interval 1h"))
	_ = as.SetEnabled("c", false)

	m.catchUpAutopilots()

	var got []string
	timeout := time.After(time.Second)
	for len(got) < 2 {
		select {
		case msg := <-capT.posts:
			slug := strings.SplitN(strings.TrimPrefix(msg.Text, "[autopilot "), "]", 2)[0]
			got = append(got, slug)
		case <-timeout:
			t.Fatalf("expected 2 posts, got %d: %v", len(got), got)
		}
	}
	if want := []string{"a", "b"}; len(got) != len(want) {
		t.Fatalf("posts = %v", got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("slug order = %v, want %v", got, want)
			}
		}
	}
	// a, b ran once; c paused never; dangling never (refused).
	if c, _ := as.Get("a"); c.LastRun.IsZero() {
		t.Fatal("a not marked ran")
	}
	if c, _ := as.Get("b"); c.LastRun.IsZero() {
		t.Fatal("b not marked ran")
	}
	if c, _ := as.Get("c"); !c.LastRun.IsZero() {
		t.Fatal("paused card ran")
	}
	if c, _ := as.Get("dangling"); !c.LastRun.IsZero() {
		t.Fatal("dangling card marked ran")
	}

	// Second catch-up posts nothing (each ran once; no double-fire).
	m.catchUpAutopilots()
	select {
	case msg := <-capT.posts:
		t.Fatalf("double-run: %+v", msg)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestAutopilotTickChainArmsAndReArms(t *testing.T) {
	m, as := autoSurface(t)
	capT := &capturingTurns{posts: make(chan bus.Message, 8)}
	m.rt = capT
	_, _ = as.Create("i", "I", "alice", "p", mustKeyboard(t, "interval 10m"))
	if err := as.MarkRan("i", autoClock(2026, 9, 2, 8, 40)); err != nil {
		t.Fatal(err)
	}

	cmd := m.armAutopilots()
	if cmd == nil {
		t.Fatal("interval card should arm a tick")
	}
	// The armed tick fires; catch-up runs the due card exactly once.
	if c := m.Update(autopilotTickMsg{}); c == nil {
		t.Fatal("tick did not re-arm")
	}
	select {
	case msg := <-capT.posts:
		if msg.Channel != "dm:alice" {
			t.Fatalf("tick posted to %q", msg.Channel)
		}
	case <-time.After(time.Second):
		t.Fatal("tick fired no run")
	}
	if c, _ := as.Get("i"); !c.LastRun.Equal(autoClock(2026, 9, 2, 9, 0)) {
		t.Fatalf("LastRun = %v, want catch-up time", c.LastRun)
	}

	// Pausing stops the chain: no tick, no re-arm.
	if err := as.SetEnabled("i", false); err != nil {
		t.Fatal(err)
	}
	if cmd := m.armAutopilots(); cmd != nil {
		t.Fatal("paused store must not arm")
	}
	select {
	case msg := <-capT.posts:
		t.Fatalf("paused card ran: %+v", msg)
	case <-time.After(20 * time.Millisecond):
	}
}
