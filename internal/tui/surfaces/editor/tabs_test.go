package editor

import "testing"

func TestExpandTabsAndVisCol(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\tx", "    x"},
		{"ab\tc", "ab  c"},
		{"abcd\te", "abcd    e"},
		{"\x1b[31m\tred\x1b[0m", "\x1b[31m    red\x1b[0m"}, // escapes are width 0
		{"no tabs", "no tabs"},
	}
	for _, c := range cases {
		if got := expandTabs(c.in); got != c.want {
			t.Errorf("expandTabs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	raw := "\tif x {\t// c"
	for col, want := range map[int]int{0: 0, 1: 4, 2: 5, 7: 10, 8: 12, 12: 16, 14: 18} {
		if got := visCol(raw, col); got != want {
			t.Errorf("visCol(col %d) = %d, want %d", col, got, want)
		}
	}
}
