package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// setupApp wires a checklist with done of total settled; running a step
// records it and queues stubGate as the step's dialog.
func setupApp(t *testing.T, done, total int) (*App, *int) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	a, _ := newTestApp(t)
	items := make([]kit.ChecklistItem, total)
	for i := range items {
		items[i] = kit.ChecklistItem{ID: string(rune('a' + i)), Title: "Step " + string(rune('A'+i))}
		if i < done {
			items[i].State = kit.ItemDone
		}
	}
	nexts := 0
	a.SetSetup(func() []kit.ChecklistItem { return items }, func(string) {
		nexts++
		a.QueueGate(&stubGate{})
	})
	return a, &nexts
}

// Every step, done ones included, has its own palette entry: the
// keyboard reaches what the start page only offers by click.
func TestPaletteOffersEveryStep(t *testing.T) {
	a, nexts := setupApp(t, 1, 2)
	a.Update(keyPress("ctrl+p"))
	v := stripAll(a.compose())
	if !strings.Contains(v, "Set up: Step A") || !strings.Contains(v, "Set up: Step B") {
		t.Fatalf("palette lacks per-step entries:\n%s", v)
	}
	for _, r := range "step a" {
		a.Update(keyPress(string(r)))
	}
	a.Update(keyPress("enter"))
	if *nexts != 1 || !a.gateActive() {
		t.Fatalf("redo a done step from the palette: next=%d gate=%v", *nexts, a.gateActive())
	}
}

// F-098: while steps are pending the status row shows "setup N/M"; a
// click on it opens the next step as a gate.
func TestSetupPillShowsAndClickRunsTheNextStep(t *testing.T) {
	a, nexts := setupApp(t, 2, 6)
	view := stripAll(a.compose())
	status := strings.Split(view, "\n")[a.height-1]
	i := strings.Index(status, "setup 2/6")
	if i < 0 {
		t.Fatalf("status row lacks the setup pill: %q", status)
	}
	x := len([]rune(status[:i])) + 2
	a.Update(tea.MouseClickMsg{X: x, Y: a.height - 1, Button: tea.MouseLeft})
	if *nexts != 1 || !a.gateActive() {
		t.Fatalf("click: next=%d gate=%v", *nexts, a.gateActive())
	}
}

func TestSetupPillHiddenWhenEverythingIsSettled(t *testing.T) {
	a, _ := setupApp(t, 6, 6)
	if strings.Contains(stripAll(a.compose()), "setup 6/6") {
		t.Fatal("a settled checklist shows no pill")
	}
	a.Update(keyPress("ctrl+p"))
	if strings.Contains(stripAll(a.compose()), "Continue setup") {
		t.Fatal("no Continue setup entry once settled")
	}
}

func TestPaletteContinueSetupStartsTheStep(t *testing.T) {
	a, nexts := setupApp(t, 1, 4)
	a.Update(keyPress("ctrl+p"))
	for _, r := range "continue" {
		a.Update(keyPress(string(r)))
	}
	a.Update(keyPress("enter"))
	if *nexts != 1 || !a.gateActive() {
		t.Fatalf("palette: next=%d gate=%v", *nexts, a.gateActive())
	}
}

func TestToastFromTheShell(t *testing.T) {
	a, _ := setupApp(t, 0, 1)
	theme.MotionForTest(t, false) // no slide-in: the toast is drawn whole
	a.Toast(kit.Toast{Title: "You're set up"})
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !strings.Contains(stripAll(a.compose()), "You're set up") {
		t.Fatal("shell toast not shown")
	}
}
