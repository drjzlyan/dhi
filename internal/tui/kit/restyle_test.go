package kit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestRestyleSurvivesInnerResets(t *testing.T) {
	row := "\x1b[31m▲\x1b[m title \x1b[2mbadge\x1b[0m end"
	st := theme.InsetBg()
	out := Restyle(st, row)
	if ansi.Strip(out) != ansi.Strip(row) {
		t.Fatalf("text changed: %q", ansi.Strip(out))
	}
	open, _, _ := strings.Cut(st.Render("\x00"), "\x00")
	// The style is re-opened after both inner resets.
	if n := strings.Count(out, open); n < 3 {
		t.Fatalf("style opened %d times, want 3 (start + after each reset): %q", n, out)
	}
	if Restyle(lipgloss.NewStyle(), "plain") != "plain" {
		t.Error("an empty style must leave the text alone")
	}
}
