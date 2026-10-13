package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestFormViewWidthFitsAndKeepsTheCursorVisible(t *testing.T) {
	long := strings.Repeat("a", 60) + "END"
	f := NewForm("t", NewTextField("first", long), NewTextField("second", long), NewToggleField("pick", []string{"one", "two"}, 0))
	for _, l := range f.ViewWidth(40) {
		if w := ansi.Width(l); w > 40 {
			t.Fatalf("row %q is %d wide", ansi.Strip(l), w)
		}
	}
	rows := f.ViewWidth(40)
	if got := ansi.Strip(rows[0]); !strings.Contains(got, "END"+theme.GlyphCursor) || !strings.Contains(got, "…") {
		t.Fatalf("active row lost its cursor: %q", got)
	}
	if got := ansi.Strip(rows[1]); !strings.HasSuffix(got, "…") {
		t.Fatalf("inactive row not ellipsized: %q", got)
	}
	short := NewForm("t", NewTextField("name", "x"))
	if a, b := ansi.Strip(strings.Join(short.ViewWidth(80), "\n")), ansi.Strip(strings.Join(short.View(), "\n")); a != b {
		t.Fatalf("short form differs from View:\n%s\n---\n%s", a, b)
	}
}
