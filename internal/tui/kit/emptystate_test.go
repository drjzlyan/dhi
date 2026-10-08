package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestEmptyStateContentCenteringAndBounds(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	es := EmptyState{Title: "No reviews yet", Why: "A review is a diff of a branch, worktree or pull request, with threaded comments.", Action: "press n to start one"}
	lines := es.Lines(60, 0)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{theme.GlyphSpark, "No reviews yet", "A review is a diff", "press n to start one"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q:\n%s", want, joined)
		}
	}
	for _, l := range lines {
		if w := len([]rune(ansi.Strip(l))); w > 60 {
			t.Errorf("row exceeds width: %d cells: %q", w, l)
		}
	}
	// The title row is centered: roughly equal margins either side.
	for _, l := range lines {
		p := ansi.Strip(l)
		if strings.Contains(p, "No reviews yet") {
			left := len(p) - len(strings.TrimLeft(p, " "))
			right := 60 - left - len("No reviews yet")
			if d := left - right; d < -1 || d > 1 {
				t.Errorf("title not centered: left %d right %d", left, right)
			}
		}
	}
}

func TestEmptyStatePadsToHeightAndTolerantWidths(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	es := EmptyState{Title: "Nothing here"}
	if got := len(es.Lines(40, 20)); got != 20 {
		t.Fatalf("height 20 → %d rows", got)
	}
	if got := len(es.Lines(40, 0)); got < 2 {
		t.Fatalf("height 0 should still emit the block, got %d rows", got)
	}
	// A tiny pane still renders without panicking or overflowing the wrap.
	for _, l := range (EmptyState{Title: "T", Why: strings.Repeat("word ", 30)}).Lines(10, 8) {
		if w := len([]rune(ansi.Strip(l))); w > 16 {
			t.Errorf("tiny pane row %d cells: %q", w, l)
		}
	}
}
