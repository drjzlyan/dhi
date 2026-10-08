package linediff

import (
	"strings"
	"testing"
)

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func glyphs(marks []Mark) string {
	var sb strings.Builder
	for _, m := range marks {
		sb.WriteByte(" +~v"[m])
	}
	return sb.String()
}

func TestDiffMarks(t *testing.T) {
	cases := []struct {
		name, old, new, want string
	}{
		{"identical", "a,b,c", "a,b,c", "   "},
		{"all new", "", "a,b", "++"},
		{"added middle", "a,c", "a,b,c", " + "},
		{"added end", "a,b", "a,b,c", "  +"},
		{"modified", "a,b,c", "a,X,c", " ~ "},
		{"modified and grown", "a,b,c", "a,X,Y,c", " ~+ "},
		{"deleted middle marks line above", "a,b,c", "a,c", "v "},
		{"deleted first marks line 0", "a,b", "b", "v"},
		{"deleted last marks last line", "a,b,c", "a,b", " v"},
		{"everything removed", "a,b", "", ""},
		{"replace block", "a,b,c,d", "a,X,Y,d", " ~~ "},
	}
	for _, c := range cases {
		got, ok := Diff(lines(c.old), lines(c.new))
		if !ok {
			t.Fatalf("%s: not ok", c.name)
		}
		if g := glyphs(got); g != c.want {
			t.Errorf("%s: marks %q, want %q", c.name, g, c.want)
		}
	}
}

func TestDiffGivesUpOnHugeInputs(t *testing.T) {
	big := make([]string, 3000)
	other := make([]string, 3000)
	for i := range big {
		big[i] = "a"
		other[i] = "b"
	}
	if _, ok := Diff(big, other); ok {
		t.Fatal("expected ok=false past the size guard")
	}
}
