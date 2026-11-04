package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// F-174: gb adds the next occurrence as a cursor; the title names the count,
// the extra cursors are painted with the theme's style, and edits hit all.
func TestMultiCursorGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := openText(t, "let count = 1\ncount += count\nreturn count\n", false)
	feed(m, "w", "g", "b", "g", "b")
	e := m.active()
	if e.CursorCount() != 3 {
		t.Fatalf("cursors = %d", e.CursorCount())
	}
	view := m.View()
	if !strings.Contains(view, "\x1b[") || !strings.Contains(view, theme.SecondaryCursor().Render("c")) {
		t.Error("the secondary cursors are not painted with the theme style")
	}
	golden.Snapshot(t, "editor_multicursor_100x30", view)
	feed(m, "c", "i", "w", "n", "esc")
	if got := e.Buffer().Text(); got != "let n = 1\nn += n\nreturn count\n" { // two presses of gb: three cursors, the fourth match untouched
		t.Errorf("text = %q", got)
	}
}

// The first esc collapses the extra cursors; only the next one leaves the buffer.
func TestMultiCursorEscCollapsesBeforeLeaving(t *testing.T) {
	m := openText(t, "a\nb\n", false)
	feed(m, "alt+down")
	if m.active().CursorCount() != 2 {
		t.Fatalf("cursors = %d", m.active().CursorCount())
	}
	feed(m, "esc")
	if m.active().CursorCount() != 1 || !m.bufFocus {
		t.Errorf("cursors %d, buffer focused %v", m.active().CursorCount(), m.bufFocus)
	}
	feed(m, "esc")
	if m.bufFocus {
		t.Error("second esc should hand focus back to the tree")
	}
}
