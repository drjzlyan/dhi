package settings

import (
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// brainSurface: an agent with private lessons plus a user preference.
func brainSurface(t *testing.T) (*Model, *lessons.Store) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	m, ws, company, _ := agentSurface(t)
	if err := company.CreateAgent(ws, &manifest.Agent{ID: "dev", Name: "Dev", Model: "m", Runtime: "claude", Tools: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	st := lessons.Open(ws.Root)
	m.d.Lessons = st
	m.now = func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) }
	// Lessons stamped two days before the view's clock: ages never drift
	// with the wall clock.
	st.SetClock(func() time.Time { return time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC) })
	add := func(l lessons.Lesson) lessons.Lesson {
		t.Helper()
		out, _, err := st.Add(l)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	add(lessons.Lesson{Scope: lessons.ScopeUser, Text: "Keep pull requests small and focused", Confidence: 0.9, Source: lessons.SourceCorrection})
	pinned := add(lessons.Lesson{Agent: "dev", Text: "Run go test with -race in this repo", Tags: []string{"go"}, Confidence: 0.85, Source: lessons.SourceReflection})
	_ = st.Set(lessons.ScopePrivate, "dev", pinned.ID, func(l *lessons.Lesson) { l.Status = lessons.StatusPinned; l.Uses = 6 })
	add(lessons.Lesson{Agent: "dev", Text: "Distribute the remainder one cent at a time", Confidence: 0.6, Source: lessons.SourceReflection})
	old := add(lessons.Lesson{Agent: "dev", Text: "Use the legacy money package", Confidence: 0.3, Source: lessons.SourceAgent})
	_ = st.Set(lessons.ScopePrivate, "dev", old.ID, func(l *lessons.Lesson) { l.Status = lessons.StatusRetired })
	for m.sec != secBrain {
		feed(m, "]")
	}
	m.Resize(120, 26)
	return m, st
}

func TestBrainGolden(t *testing.T) {
	m, _ := brainSurface(t)
	golden.Snapshot(t, "settings_brain", m.View())
	golden.AssertBgIntegrity(t, "brain", m.View())
}

func TestBrainPinRetireShareFilter(t *testing.T) {
	m, st := brainSurface(t)
	view := ansi.Strip(m.View())
	for _, want := range []string{"YOUR PREFERENCES", "DEV", "standard?", "×6 · reflection", "4 lesson(s)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("brain missing %q:\n%s", want, view)
		}
	}
	// Cursor 0 = the user preference: pin it.
	feed(m, "p")
	if !strings.Contains(m.flash, "pinned") {
		t.Fatalf("flash = %q", m.flash)
	}
	user, _ := st.List("")
	if user[0].Status != lessons.StatusPinned {
		t.Fatalf("pin not stored: %+v", user[0])
	}
	// Down to "Distribute…" (pinned dev lesson sorts by recency; find it).
	for i := 0; i < 4; i++ {
		if l := m.brainSelected(); l != nil && strings.HasPrefix(l.Text, "Distribute") {
			break
		}
		feed(m, "j")
	}
	feed(m, "x")
	if !strings.Contains(m.flash, "retired") {
		t.Fatalf("retire flash = %q", m.flash)
	}
	var shared lessons.Lesson
	m.d.ShareLesson = func(l lessons.Lesson) (string, error) { shared = l; return "queued for review", nil }
	feed(m, "s")
	if !strings.HasPrefix(shared.Text, "Distribute") || !strings.Contains(m.flash, "queued for review") {
		t.Fatalf("share = %+v flash %q", shared, m.flash)
	}
	// Filter narrows to matching lessons.
	feed(m, "/", "r", "a", "c", "e", "enter")
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "filter: race") || !strings.Contains(view, "1 lesson(s)") || strings.Contains(view, "Distribute") {
		t.Fatalf("filtered view:\n%s", view)
	}
}

func TestBrainEmptyExplainsLearning(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m, ws, _, _ := agentSurface(t)
	m.d.Lessons = lessons.Open(ws.Root)
	for m.sec != secBrain {
		feed(m, "]")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "agents learn when a check fails") {
		t.Fatalf("empty brain must explain how learning happens:\n%s", view)
	}
}

func TestBrainMemoryView(t *testing.T) {
	m, _ := brainSurface(t)
	mem := memory.Open(m.d.WS)
	m.d.Memory = mem
	_ = mem.Append("dev", "decision", "use int64 cents everywhere")
	_ = mem.WriteNotes("dev", "# Dev notes\n- the ledger lives in app/ledger")
	for m.brainSelected() == nil || m.brainSelected().Agent != "dev" {
		feed(m, "j")
	}
	feed(m, "m")
	if m.dlg == nil {
		t.Fatal("m did not open the memory view")
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"memory · dev", "JOURNAL", "use int64 cents everywhere", "NOTES", "the ledger lives in app/ledger"} {
		if !strings.Contains(view, want) {
			t.Fatalf("memory view missing %q:\n%s", want, view)
		}
	}
	// The pinned cap comes from settings.
	feed(m, "esc")
	m.cfg.Memory.PinnedMax = 1
	l := m.brainSelected()
	if l.Status != lessons.StatusPinned {
		feed(m, "p")
		if !strings.Contains(m.flash, "memory.pinned_max") {
			t.Fatalf("pin past the cap: flash %q", m.flash)
		}
	}
}
