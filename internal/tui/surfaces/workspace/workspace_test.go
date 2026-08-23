package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestMetaIsBootSurface(t *testing.T) {
	m := New("test")
	if got := m.Meta(); got.ID != "workspace" || got.Title != "Workspace" {
		t.Fatalf("meta = %+v", got)
	}
	var _ surfaces.Surface = m
}

func TestViewRendersCenteredHeroAndCapabilities(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := New("0.1.0")
	m.Resize(100, 30)
	out := m.View()

	for _, want := range []string{"███████", "#general", "marketplace"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if w := len([]rune(ansi.Strip(l))); w > 100 {
			t.Fatalf("line %d exceeds width: %d", i, w)
		}
	}
}
