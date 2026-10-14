package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestToastRendersAndFits(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	v := Toast{Title: "ship-docs shipped", Detail: "3 runs · $0.42"}.View(80, 1)
	if got := ansi.Strip(v); !strings.Contains(got, "✓ ship-docs shipped") || !strings.Contains(got, "3 runs") {
		t.Fatalf("toast = %q", got)
	}
	if w := ansi.Width(Toast{Title: strings.Repeat("x", 90)}.View(40, 1)); w > 40 {
		t.Fatalf("toast width %d > 40", w)
	}
	if w := ansi.Width(Toast{Title: "shipped"}.View(80, 0.5)); w >= ansi.Width(Toast{Title: "shipped"}.View(80, 1)) {
		t.Fatal("a half-revealed toast should be narrower")
	}
}

// TestGoldenToast pins the toast whole (reduced motion shows it so at
// once) and mid slide-in (motion on, frame 2 of 4).
func TestGoldenToast(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	tt := Toast{Title: "ship-docs shipped", Detail: "3 runs · $0.42"}
	golden.Snapshot(t, "toast_reduced", tt.View(80, 1))
	golden.Snapshot(t, "toast_motion_f2", tt.View(80, 0.5))
}
