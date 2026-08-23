package branding

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestHeroBlockContainsBrandElements(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	block := HeroBlock("9.9.9")
	for _, want := range []string{"██████╗", "the agentic workspace IDE", "v9.9.9"} {
		if !strings.Contains(block, want) {
			t.Fatalf("hero block missing %q", want)
		}
	}
}

func TestHeroCentersWithinWidth(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	out := Hero(80, 12, "0.1.0")
	lines := strings.Split(out, "\n")
	if len(lines) > 12 {
		t.Fatalf("hero height %d exceeds 12", len(lines))
	}
	for i, l := range lines {
		if w := len([]rune(ansi.Strip(l))); w > 80 {
			t.Fatalf("line %d visible width %d exceeds 80", i, w)
		}
	}
}
