package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

func autoClock(y, m, d, hh, mm int) time.Time {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC)
}

// autoSurface seeds a workspace with a bus + rogue-free roster and a
// chip of autopilots; the clock is frozen for deterministic due math.
func autoSurface(t *testing.T, cards map[string]string) (*Model, *autopilot.Store) {
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

func gotoAutopilots(m *Model) {
	for i := secMembers; i < secAutopilots; i++ {
		m.HandleKey("]")
	}
}

func TestAutopilotsEmptyState(t *testing.T) {
	m, _ := autoSurface(t, nil)
	gotoAutopilots(m)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "no autopilots") {
		t.Fatalf("empty state missing hint:\n%s", out)
	}
}

func TestAutopilotsEmptyStateGolden(t *testing.T) {
	m, _ := autoSurface(t, nil)
	gotoAutopilots(m)
	golden.Snapshot(t, "workspace_autopilots_empty", m.View())
}

func TestAutopilotKeyNewFormAndRemove(t *testing.T) {
	m, as := autoSurface(t, nil)
	_, _ = as.Create("standup", "Morning standup", "alice", "summarize", mustKeyboard(t, "daily 09:00"))
	gotoAutopilots(m)

	if !m.HandleKey("n") {
		t.Fatal("n not consumed")
	}
	if m.form.kind != fAutoNew || len(m.form.fields) != 5 {
		t.Fatalf("n did not open the new form: kind=%v fields=%d", m.form.kind, len(m.form.fields))
	}
	m.HandleKey("esc")
	if m.form.kind != fNone {
		t.Fatal("esc did not close the form")
	}

	if !m.HandleKey("x") {
		t.Fatal("x not consumed")
	}
	if m.form.kind != fAutoDeleteConfirm {
		t.Fatalf("x did not open remove confirm: kind=%v", m.form.kind)
	}
	m.HandleKey("enter")
	if _, ok := as.Get("standup"); ok {
		t.Fatal("remove confirm left the card")
	}
}

func TestAutopilotKeyOpenLastTranscript(t *testing.T) {
	m, ws := newSurface(t)
	m.roster = &stubRoster{ids: []string{"alice"}}
	m.taskStore = seededRunStore(t, ws) // alice has run-replay-test with a transcript
	m.now = func() time.Time { return autoClock(2026, 9, 2, 9, 0) }
	as, err := autopilot.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.autopilots = as
	_, _ = as.Create("standup", "Morning standup", "alice", "summarize", mustKeyboard(t, "daily 09:00"))
	_ = as.MarkRan("standup", autoClock(2026, 9, 1, 9, 0))
	gotoAutopilots(m)
	if !m.HandleKey("o") {
		t.Fatal("o not consumed")
	}
	if m.replay == nil {
		t.Fatal("o did not open the last transcript")
	}
	if !strings.Contains(ansi.Strip(m.View()), "go test ./...") {
		t.Fatalf("replay body missing transcript:\n%s", ansi.Strip(m.View())[:400])
	}
}

func TestAutopilotsSectionGolden(t *testing.T) {
	m, as := autoSurface(t, nil)
	_, _ = as.Create("standup", "Morning standup", "alice", "summarize", mustKeyboard(t, "daily 09:00"))
	_, _ = as.Create("audit", "Friday audit", "bob", "audit", mustKeyboard(t, "weekly fri 17:00"))
	if err := as.MarkRan("standup", autoClock(2026, 9, 1, 9, 0)); err != nil {
		t.Fatal(err)
	}
	_ = as.SetEnabled("audit", false)
	gotoAutopilots(m)
	golden.Snapshot(t, "workspace_autopilots", m.View())
}

type capturingTurns struct{ posts chan bus.Message }

func (c *capturingTurns) Handle(_ context.Context, msg bus.Message) {
	select {
	case c.posts <- msg:
	default:
	}
}

func TestAutopilotRunNowPostsToDm(t *testing.T) {
	m, as := autoSurface(t, nil)
	capT := &capturingTurns{posts: make(chan bus.Message, 4)}
	m.rt = capT
	_, _ = as.Create("standup", "Morning standup", "alice", "summarize", mustKeyboard(t, "daily 09:00"))
	gotoAutopilots(m)
	if !m.HandleKey("r") {
		t.Fatal("r not consumed")
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
	c, _ := as.Get("standup")
	if c.LastRun.IsZero() {
		t.Fatal("run-now did not mark ran")
	}
}

func TestAutopilotDanglingAgentRefuses(t *testing.T) {
	m, as := autoSurface(t, nil)
	capT := &capturingTurns{posts: make(chan bus.Message, 4)}
	m.rt = capT
	_, _ = as.Create("ghost", "Ghost", "ghost", "p", mustKeyboard(t, "daily 09:00"))
	gotoAutopilots(m)
	if err := m.runAutopilotNow("ghost"); err == nil ||
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
	m, as := autoSurface(t, nil)
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
	m, as := autoSurface(t, nil)
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

func TestAutopilotKeyToggleArm(t *testing.T) {
	m, as := autoSurface(t, nil)
	_, _ = as.Create("standup", "Morning standup", "alice", "summarize", mustKeyboard(t, "daily 09:00"))
	gotoAutopilots(m)
	if !m.HandleKey("e") {
		t.Fatal("e not consumed")
	}
	c, _ := as.Get("standup")
	if c.Enabled {
		t.Fatal("e did not pause the card")
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "paused") {
		t.Fatalf("paused state not rendered:\n%s", out)
	}
	if !m.HandleKey("e") {
		t.Fatal("e (arm) not consumed")
	}
	c, _ = as.Get("standup")
	if !c.Enabled {
		t.Fatal("e did not re-arm the card")
	}
}
