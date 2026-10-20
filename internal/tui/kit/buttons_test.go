package kit

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestButtonsWrapWholeAndReportSpans(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	lines, spans := Buttons([]Button{{Key: "y", Label: "yes"}, {Key: "o", Label: "once"}, {Key: "n", Label: "no"}}, 18)
	if len(lines) != 2 {
		t.Fatalf("lines = %q, want 2 (no button split)", lines)
	}
	for _, l := range lines {
		if w := ansi.Width(l); w > 18 {
			t.Fatalf("line wider than 18: %q", ansi.Strip(l))
		}
	}
	last := spans[2]
	if last.Line != 1 || last.Key != "n" || ansi.Strip(lines[1])[last.From:last.To] != " n no " {
		t.Fatalf("span = %+v on %q", last, ansi.Strip(lines[1]))
	}
}

func TestButtonFaceWidthMatchesRender(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	for _, b := range []Button{{Key: "⏎", Label: "Continue"}, {Label: "Cancel", Variant: theme.ButtonGhost}, {Key: "d", Label: "Delete", Variant: theme.ButtonDanger, Focused: true}} {
		if got := ansi.Width(b.Render()); got != b.Width() {
			t.Errorf("%+v: rendered %d cells, Width() %d", b, got, b.Width())
		}
	}
}

func TestDisabledButtonHasNoSpan(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	_, spans := Buttons([]Button{{Key: "i", Label: "Install", Disabled: true}, {Key: "s", Label: "Skip"}}, 80)
	if len(spans) != 1 || spans[0].Key != "s" {
		t.Fatalf("spans = %+v, want only s", spans)
	}
}
