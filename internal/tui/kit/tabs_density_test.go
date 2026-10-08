package kit

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
)

func TestTabsCompactBeforeClipping(t *testing.T) {
	tabs := NewTabs([2]string{"ws", "Workspace"}, [2]string{"ed", "Editor"}, [2]string{"id", "Ideator"},
		[2]string{"rv", "Reviewer"}, [2]string{"st", "Settings"})
	tabs.Active = 1
	cases := []struct {
		width      int
		want, deny []string
	}{
		{120, []string{"1 Workspace", "2 Editor", "5 Settings"}, nil},
		{50, []string{"2 Editor", " 1 ", " 5 "}, []string{"Workspace", "…"}},
		{30, []string{" 1 ", " 2 ", " 5 "}, []string{"Editor", "…"}},
	}
	for _, c := range cases {
		tabs.Width = c.width
		got := ansi.Strip(tabs.View())
		if w := ansi.Width(got); w != c.width {
			t.Errorf("width %d: bar is %d cells", c.width, w)
		}
		for _, s := range c.want {
			if !strings.Contains(got, s) {
				t.Errorf("width %d: %q missing from %q", c.width, s, got)
			}
		}
		for _, s := range c.deny {
			if strings.Contains(got, s) {
				t.Errorf("width %d: %q should be compacted away in %q", c.width, s, got)
			}
		}
		// Hit agrees with the compacted layout.
		col := strings.Index(got, " 5 ")
		if c.width == 120 {
			col = strings.Index(got, "5 Settings")
		}
		if i, ok := tabs.Hit(len([]rune(got[:col])) + 1); !ok || i != 4 {
			t.Errorf("width %d: Hit on tab 5 = %d,%v", c.width, i, ok)
		}
	}
}
