package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// TestRailGroupsHeadRowsAndSkipClicks (F-093): a heading precedes each
// new group, rows keep their indexes for Active and RowAt, a heading is
// not clickable, and scrolling keeps the active row's heading in view.
func TestRailGroupsHeadRowsAndSkipClicks(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	r := &Rail{Width: 20, Active: 2, Rows: []RailRow{
		{Label: "A", Group: "one"}, {Label: "B", Group: "one"}, {Label: "C", Group: "two"},
	}}
	lines := strings.Split(ansi.Strip(r.View()), "\n")
	if len(lines) != 5 || !strings.Contains(lines[0], "one") || !strings.Contains(lines[3], "two") {
		t.Fatalf("lines = %q", lines)
	}
	if _, ok := r.RowAt(0); ok {
		t.Fatal("heading was clickable")
	}
	if i, ok := r.RowAt(4); !ok || i != 2 {
		t.Fatalf("RowAt(4) = %d,%v want 2", i, ok)
	}
	r.Height = 2
	lines = strings.Split(ansi.Strip(r.View()), "\n")
	if !strings.Contains(lines[0], "two") || !strings.Contains(lines[1], "C") {
		t.Fatalf("scrolled window lost the heading: %q", lines)
	}
}
