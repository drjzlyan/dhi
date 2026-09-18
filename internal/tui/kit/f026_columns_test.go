package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestColumnsLaneScrollFollowsCursor(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	c := &Columns{
		Active: 0,
		Width:  40,
		Height: 3,
		Cols: []Column{
			{Title: "lane", Cursor: 0, Rows: []string{"a", "b", "c", "d", "e"}},
			{Title: "empty"},
		},
	}
	if w := c.LaneWidth(0); w != 20 {
		t.Fatalf("LaneWidth = %d, want 20", w)
	}
	view := ansi.Strip(c.View())
	// Head window shows a-c, not d-e.
	if strings.Contains(view, "\nd\n") || strings.Contains(view, "\ne\n") {
		t.Fatalf("head window leaked rows: %q", view)
	}
	// Cursor follows into the tail: cursor 4 keeps "e" visible.
	c.Cols[0].Cursor = 4
	view = ansi.Strip(c.View())
	if !strings.Contains(view, "e") {
		t.Fatalf("cursor row not kept visible: %q", view)
	}
	if strings.Contains(view, "\na\n") && strings.Contains(view, "\nb\n") {
		t.Fatalf("head rows not scrolled out: %q", view)
	}
}

func TestColumnsEmptyLanePlaceholder(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	c := &Columns{Width: 40, Height: 2, Cols: []Column{
		{Title: "x"}, {Title: "y", Rows: []string{"r1"}},
	}}
	view := ansi.Strip(c.View())
	if !strings.Contains(view, "—") {
		t.Fatalf("empty placeholder missing: %q", view)
	}
	if !strings.Contains(view, "r1") {
		t.Fatalf("populated row missing: %q", view)
	}
}
