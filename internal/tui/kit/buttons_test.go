package kit

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestButtonsWrapWholeAndReportSpans(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	lines, spans := Buttons([]Button{{"y", "yes"}, {"o", "once"}, {"n", "no"}}, 18)
	if len(lines) != 2 {
		t.Fatalf("lines = %q, want 2 (no button split)", lines)
	}
	for _, l := range lines {
		if w := ansi.Width(l); w > 18 {
			t.Fatalf("line wider than 18: %q", ansi.Strip(l))
		}
	}
	last := spans[2]
	if last.Line != 1 || last.Key != "n" || ansi.Strip(lines[1])[last.From:last.To] != " n  no" {
		t.Fatalf("span = %+v on %q", last, ansi.Strip(lines[1]))
	}
}
