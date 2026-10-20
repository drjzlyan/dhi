package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

type startSpy struct {
	ran   []string
	tour  int
	tasks int
	items []kit.ChecklistItem
}

func (s *startSpy) deps() StartDeps {
	return StartDeps{
		Checklist: func() []kit.ChecklistItem { return s.items },
		RunStep:   func(id string) { s.ran = append(s.ran, id) },
		StartTour: func() { s.tour++ },
		NewTask:   func() { s.tasks++ },
	}
}

func newStartEditor(t *testing.T) (*Model, *startSpy) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	spy := &startSpy{items: []kit.ChecklistItem{
		{ID: "identity", Title: "Git identity", Why: "sign your commits", State: kit.ItemDone},
		{ID: "you", Title: "How you like to work", Why: "agents write the way you read"},
		{ID: "team", Title: "Pick a team", Why: "agents that take tasks"},
	}}
	m := New("test", ws, WithStart(spy.deps()))
	m.Resize(120, 30)
	return m, spy
}

func TestStartPageGolden(t *testing.T) {
	m, _ := newStartEditor(t)
	golden.Snapshot(t, "editor_start_120x30", plainView(m))
}

// The start page is a focus zone that does not capture input, so the
// shell's digit and tab view switches work on DHI's first screen (F-097).
func TestStartPageLeavesDigitsToTheShell(t *testing.T) {
	m, _ := newStartEditor(t)
	if !m.startVisible() || m.CapturesInput() {
		t.Fatalf("start visible=%v captures=%v", m.startVisible(), m.CapturesInput())
	}
}

func TestStartPageKeysRunTheirActions(t *testing.T) {
	m, spy := newStartEditor(t)
	feed(m, "c", "t", "n")
	if strings.Join(spy.ran, ",") != "you" || spy.tour != 1 || spy.tasks != 1 {
		t.Fatalf("ran=%v tour=%d tasks=%d", spy.ran, spy.tour, spy.tasks)
	}
	if _, hints := m.Hints(); hints[0] != "c continue setup" {
		t.Fatalf("hints = %v", hints)
	}
	spy.items[1].State, spy.items[2].State = kit.ItemDone, kit.ItemSkipped
	if strings.Contains(plainView(m), "GET SET UP") {
		t.Fatal("a settled checklist must hide the card")
	}
	feed(m, "c")
	if len(spy.ran) != 1 {
		t.Fatal("c with nothing pending must do nothing")
	}
}

// Clicks resolve against exactly what is drawn: a checklist row runs its
// step, a button presses its key.
func TestStartPageClicks(t *testing.T) {
	m, spy := newStartEditor(t)
	view := ansi.Strip(m.View())
	x, y := locate(t, view, "Pick a team")
	if !m.Click(x, y) || len(spy.ran) != 1 || spy.ran[0] != "team" {
		t.Fatalf("click on the team row: ran=%v", spy.ran)
	}
	x, y = locate(t, view, "Find a file")
	m.Click(x, y)
	if m.mode != modeFind {
		t.Fatalf("click on Find a file: mode=%v", m.mode)
	}
}

func TestStartPageWithoutAWorkspace(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	spy := &startSpy{items: []kit.ChecklistItem{{ID: "workspace", Title: "Create a workspace", Why: "group your repos"}}}
	m := New("test", nil, WithStart(spy.deps()))
	m.Resize(100, 34)
	v := plainView(m)
	if !strings.Contains(v, "Create a workspace") || strings.Contains(v, "Find a file") {
		t.Fatalf("no-workspace start page:\n%s", v)
	}
	feed(m, "c")
	if len(spy.ran) != 1 || spy.ran[0] != "workspace" {
		t.Fatalf("c must start the workspace step: %v", spy.ran)
	}
	x, y := locate(t, v, "Create a workspace")
	spy.ran = nil
	if !m.Click(x, y) || len(spy.ran) != 1 {
		t.Fatalf("click must start the workspace step: %v", spy.ran)
	}
}

// locate returns the screen cell of the first occurrence of s.
func locate(t *testing.T, view, s string) (int, int) {
	t.Helper()
	for y, l := range strings.Split(view, "\n") {
		if i := strings.Index(l, s); i >= 0 {
			return ansi.Width(l[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", s, view)
	return 0, 0
}

func TestFocusedTerminalKeepsAltDigits(t *testing.T) {
	m, _ := newStartEditor(t)
	if m.TakesAltDigits() {
		t.Fatal("the start page leaves alt+N to the shell")
	}
	m.drawerOpen, m.termFocus = true, true
	if !m.TakesAltDigits() {
		t.Fatal("a focused terminal keeps alt+1..9 for its tabs")
	}
}
