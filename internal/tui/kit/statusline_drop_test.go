package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ADR-0041: at narrow widths every hint drops before a droppable left
// segment, the highest Drop goes first, and the mode block and an
// undroppable pill (waiting) always stay.
func TestStatusLineDropsLeftSegmentsLast(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	mk := func(w int) string {
		sl := DefaultStatusLine("Editor")
		sl.Left = append(sl.Left,
			StatusSegment{Text: " ◆ 2 waiting "},
			StatusSegment{Text: " setup 2/6 ", Drop: 1, Key: "setup"},
			StatusSegment{Text: " ● 3 working ", Drop: 2})
		sl.Hints = []string{"enter open", "/ find", "s search"}
		sl.Width = w
		return ansi.Strip(sl.View())
	}
	wide := mk(160)
	for _, s := range []string{"EDITOR", "waiting", "setup 2/6", "working", "find"} {
		if !strings.Contains(wide, s) {
			t.Fatalf("wide row lacks %q: %q", s, wide)
		}
	}
	mid := mk(44)
	if strings.Contains(mid, "find") || strings.Contains(mid, "working") || !strings.Contains(mid, "setup 2/6") {
		t.Fatalf("44 cols: hints, then working, go first: %q", mid)
	}
	narrow := mk(30)
	if strings.Contains(narrow, "setup") || !strings.Contains(narrow, "EDITOR") || !strings.Contains(narrow, "waiting") {
		t.Fatalf("30 cols: mode block and waiting must stay: %q", narrow)
	}
}

func TestStatusLineSegmentKeyIsClickable(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	sl := DefaultStatusLine("Editor")
	sl.Left = append(sl.Left, StatusSegment{Text: " setup 1/3 ", Key: "setup", Drop: 1})
	sl.Width = 120
	row := ansi.Strip(sl.View())
	x := strings.Index(row, "setup")
	if k, global, ok := sl.KeyAt(x); !ok || !global || k != "setup" {
		t.Fatalf("KeyAt(%d) = %q %v %v", x, k, global, ok)
	}
}
