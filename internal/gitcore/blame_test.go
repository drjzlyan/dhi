package gitcore

import (
	"testing"
	"time"
)

func TestParseBlame(t *testing.T) {
	const a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const z = "0000000000000000000000000000000000000000"
	out := a + " 1 1 2\nauthor Ada\nauthor-mail <a@x>\nauthor-time 1700000000\nsummary first change\nfilename f\n\tone\n" +
		a + " 2 2\n\ttwo\n" +
		z + " 3 3 1\nauthor Not Committed Yet\nauthor-time 1700000100\nsummary Version of f from f\nfilename f\n\tthree\n"
	got := ParseBlame(out)
	if len(got) != 3 {
		t.Fatalf("want 3 lines, got %d: %+v", len(got), got)
	}
	if got[1].Author != "Ada" || got[1].Subject != "first change" || got[1].Text != "two" ||
		!got[1].When.Equal(time.Unix(1700000000, 0)) || got[1].Uncommitted {
		t.Fatalf("second line must reuse the first line's commit details: %+v", got[1])
	}
	if !got[2].Uncommitted || got[2].Text != "three" {
		t.Fatalf("zero hash is uncommitted: %+v", got[2])
	}
}
