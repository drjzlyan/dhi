package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestNoticeNamesWhatWhyAndFix(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	n := Notice{What: "Tasks are unavailable", Why: "the task store could not open .dhi/tasks", Retry: "run dhi doctor"}
	lines := n.Lines(50, 12)
	if len(lines) != 12 {
		t.Fatalf("rows = %d, want 12", len(lines))
	}
	all := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"✗", "Tasks are unavailable", "task store", "run dhi doctor"} {
		if !strings.Contains(all, want) {
			t.Errorf("notice missing %q:\n%s", want, all)
		}
	}
	for i, l := range lines {
		if w := ansi.Width(l); w != 50 {
			t.Errorf("row %d is %d cells, want 50", i, w)
		}
	}
	if !strings.Contains(ansi.Strip(Notice{Kind: NoticeWarning, What: "x"}.String(20)), "▲") {
		t.Error("warning glyph missing")
	}
}

func TestLoadingSkeleton(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	lines := Loading("loading diff…", 0, 40, 3)
	if len(lines) != 5 || !strings.Contains(ansi.Strip(lines[0]), "loading diff…") {
		t.Fatalf("loading = %q", lines)
	}
	for _, l := range lines {
		if ansi.Width(l) > 40 {
			t.Errorf("skeleton row wider than 40: %d", ansi.Width(l))
		}
	}
}
