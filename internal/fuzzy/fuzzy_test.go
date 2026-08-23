package fuzzy

import (
	"slices"
	"testing"
)

func TestMatchSubsequence(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"mn", "main.go", true},
		{"mng", "main.go", true},
		{"mg", "main.go", true},  // sparse subsequence still matches
		{"gm", "main.go", false}, // order matters
		{"xyz", "main.go", false},
		{"", "anything", true},
		{"GO", "main.go", true},
	}
	for _, c := range cases {
		if _, got := Match(c.pattern, c.s); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestMatchPrefersBoundariesAndContiguity(t *testing.T) {
	sBoundary, _ := Match("app", "src/app.go")
	sMid, _ := Match("app", "grapple.txt")
	if sBoundary <= sMid {
		t.Errorf("boundary score %d should beat mid-word %d", sBoundary, sMid)
	}

	sContig, _ := Match("read", "readme.md")
	sSpread, _ := Match("read", "r-e-a-d.me")
	if sContig <= sSpread {
		t.Errorf("contiguous %d should beat spread %d", sContig, sSpread)
	}
}

func TestRankOrdering(t *testing.T) {
	items := []string{
		"beta/grape.txt",
		"alpha/app.go",
		"docs/maple.md",
		"alpha/apple_test.go",
	}
	got := Rank("app", items)
	if len(got) != 2 {
		t.Fatalf("ranked %d results, want 2", len(got))
	}
	if got[0].Index != 1 {
		t.Errorf("top result = %q, want alpha/app.go", items[got[0].Index])
	}

	all := Rank("", items)
	idxs := []int{}
	for _, r := range all {
		idxs = append(idxs, r.Index)
	}
	if !slices.Equal(idxs, []int{0, 1, 2, 3}) {
		t.Errorf("empty pattern must keep input order, got %v", idxs)
	}
}
