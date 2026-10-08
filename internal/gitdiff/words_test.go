package gitdiff

import (
	"strings"
	"testing"
)

// spans renders the marked runes of s as [word] so tests read naturally.
func spans(s string, m []bool) string {
	var sb strings.Builder
	in := false
	for i, r := range []rune(s) {
		if m[i] && !in {
			sb.WriteByte('[')
			in = true
		}
		if !m[i] && in {
			sb.WriteByte(']')
			in = false
		}
		sb.WriteRune(r)
	}
	if in {
		sb.WriteByte(']')
	}
	return sb.String()
}

func TestWordMarksHighlightOnlyWhatChanged(t *testing.T) {
	cases := []struct{ old, new, wantOld, wantNew string }{
		{"return count + 1", "return total + 1", "return [count] + 1", "return [total] + 1"},
		{"func Hello(name string) error {", "func Hello(name string, loud bool) error {",
			"func Hello(name string) error {", "func Hello(name string[, loud bool]) error {"},
		{"timeout := 30", "timeout := 60", "timeout := [30]", "timeout := [60]"},
		{"foo.Bar(x)", "foo.Baz(x)", "foo.[Bar](x)", "foo.[Baz](x)"},
	}
	for _, c := range cases {
		am, bm, ok := diffWords(c.old, c.new)
		if !ok {
			t.Errorf("%q → %q produced no marks", c.old, c.new)
			continue
		}
		if got := spans(c.old, am); got != c.wantOld {
			t.Errorf("old: %q, want %q", got, c.wantOld)
		}
		if got := spans(c.new, bm); got != c.wantNew {
			t.Errorf("new: %q, want %q", got, c.wantNew)
		}
	}
}

func TestWordMarksSkipPointlessCases(t *testing.T) {
	for _, c := range [][2]string{
		{"same line", "same line"},                 // identical
		{"alpha beta gamma", "one two three four"}, // rewritten
		{"", "added"},
	} {
		if _, _, ok := diffWords(c[0], c[1]); ok {
			t.Errorf("%q → %q should carry no word marks", c[0], c[1])
		}
	}
	long := strings.Repeat("x ", maxWordTokens+5)
	if _, _, ok := diffWords(long, long+"y"); ok {
		t.Error("an over-long line must be skipped, not allocate a huge table")
	}
}

func TestWordMarksPairDelsWithAddsInAReplaceBlock(t *testing.T) {
	h := Hunk{Lines: []Line{
		{Kind: Ctx, OldNo: 1, NewNo: 1, Text: "package a"},
		{Kind: Del, OldNo: 2, Text: "x := compute(a)"},
		{Kind: Del, OldNo: 3, Text: "y := 1"},
		{Kind: Add, NewNo: 2, Text: "x := compute(b)"},
		{Kind: Add, NewNo: 3, Text: "y := 2"},
		{Kind: Add, NewNo: 4, Text: "z := 3"}, // unpaired
	}}
	marks := WordMarks(h)
	if got := spans("x := compute(a)", marks[MarkKey{Del, 2}]); got != "x := compute([a])" {
		t.Errorf("first del = %q", got)
	}
	if got := spans("y := 2", marks[MarkKey{Add, 3}]); got != "y := [2]" {
		t.Errorf("second add = %q", got)
	}
	if _, ok := marks[MarkKey{Add, 4}]; ok {
		t.Error("an unpaired added line got word marks")
	}
	if _, ok := marks[MarkKey{}]; ok {
		t.Error("context got a mark")
	}
}
