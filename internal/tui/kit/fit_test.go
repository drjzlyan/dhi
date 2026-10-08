package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
)

func TestFitPadsClipsAndCloses(t *testing.T) {
	cases := []struct {
		name  string
		block string
		w, h  int
	}{
		{"short block padded", "a\nb", 10, 5},
		{"tall block clipped", "1\n2\n3\n4\n5\n6", 10, 3},
		{"wide line clipped", strings.Repeat("x", 40), 12, 1},
		{"styled wide line", "\x1b[31m" + strings.Repeat("y", 40) + "\x1b[0m", 8, 2},
		{"wide runes", strings.Repeat("界", 10), 7, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Split(Fit(c.block, c.w, c.h), "\n")
			if len(got) != c.h {
				t.Fatalf("lines = %d, want %d", len(got), c.h)
			}
			for i, l := range got {
				if w := ansi.Width(l); w > c.w {
					t.Errorf("line %d width %d > %d: %q", i, w, c.w, l)
				}
			}
		})
	}
	if got := Fit("\x1b[31m"+strings.Repeat("z", 20), 5, 1); !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("clipped styled line must reset SGR: %q", got)
	}
	if Fit("anything", 10, 0) != "" {
		t.Error("h=0 must render nothing")
	}
}

func TestCenterFillsTheArea(t *testing.T) {
	got := strings.Split(Center("hello\nworld", 20, 9), "\n")
	if len(got) != 9 {
		t.Fatalf("Center lines = %d, want 9 (bottom must be padded too)", len(got))
	}
	if strings.TrimSpace(ansi.Strip(got[3])) != "hello" {
		t.Errorf("block not vertically centered: %q", got)
	}
	for i, l := range strings.Split(Center(strings.Repeat("w", 50), 20, 3), "\n") {
		if ansi.Width(l) > 20 {
			t.Errorf("line %d wider than the area: %d", i, ansi.Width(l))
		}
	}
}
