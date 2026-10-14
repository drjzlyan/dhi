package kit

import "testing"

func TestCount(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{0, "0 tasks"}, {1, "1 task"}, {2, "2 tasks"}} {
		if got := Count(c.n, "task"); got != c.want {
			t.Errorf("Count(%d) = %q, want %q", c.n, got, c.want)
		}
	}
	if got := Count(2, "child", "children"); got != "2 children" {
		t.Errorf("irregular = %q", got)
	}
}
