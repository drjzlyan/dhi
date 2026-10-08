package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// findOnScreen returns the cell (x, y) where needle starts in the view.
func findOnScreen(t *testing.T, view, needle string) (int, int) {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(view), "\n") {
		if i := strings.Index(l, needle); i >= 0 {
			return ansi.Width(l[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", needle, ansi.Strip(view))
	return 0, 0
}

// TestClicksSelectCardsAndSections pins F-055: clicking a card selects
// it (docked and compact), the strip switches sections below the dock
// width, and a click while a dialog is open does nothing.
func TestClicksSelectCardsAndSections(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"alpha-task", "beta-task", "gamma-task"} {
		if err := store.Create(s, "Title "+s, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	m.taskStore = store
	m.sec = secBoard

	for _, width := range []int{110, 70} {
		m.Resize(width, 30)
		m.boardActive, m.boardCur = 0, [4]int{}
		x, y := findOnScreen(t, m.View(), "Title gamma")
		if !m.Click(x, y) {
			t.Fatalf("width %d: click on a card was not handled", width)
		}
		if g := m.boardGroups(); m.boardCur[0] >= len(g[0]) || g[0][m.boardCur[0]].Slug != "gamma-task" {
			t.Fatalf("width %d: click selected row %d, want gamma-task", width, m.boardCur[0])
		}
	}

	// Compact strip: clicking REPOS switches the section.
	m.Resize(70, 30)
	x, y := findOnScreen(t, m.View(), "REPOS")
	if !m.Click(x, y) || m.sec != secRepos {
		t.Fatalf("strip click: sec = %v, want REPOS", m.sec)
	}

	// A dialog owns the screen: clicks are ignored.
	m.sec = secBoard
	m.HandleKey("n")
	before := m.boardCur
	x, y = findOnScreen(t, m.View(), "BOARD")
	if m.Click(x, y) || m.boardCur != before {
		t.Fatal("click went through an open dialog")
	}
}

// TestHintBarKeysAreClickable: clicking "n new" on the keymap row opens
// the new-task dialog, exactly like pressing n.
func TestHintBarKeysAreClickable(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard
	x, y := findOnScreen(t, m.View(), "n new")
	if !m.Click(x, y) || m.form.kind == fNone {
		t.Fatalf("clicking the n hint did not open the new-task dialog (form=%v)", m.form.kind)
	}
}
