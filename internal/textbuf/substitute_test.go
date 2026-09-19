package textbuf

import (
	"strings"
	"testing"
)

func TestSubstituteCursorLineAndAll(t *testing.T) {
	b := New("alpha beta\nbeta alpha\nalpha\n")
	// Cursor line 0: first hit only.
	b.SetCursor(Pos{Line: 0, Col: 0})
	lines, hits := b.SubstituteAll("alpha", "A", false, false)
	if lines != 1 || hits != 1 {
		t.Fatalf("cursor sub = %d/%d", lines, hits)
	}
	if got := b.Line(0); got != "A beta" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := b.Line(2); got != "alpha" {
		t.Fatalf("other line mutated: %q", got)
	}
	// Whole buffer, global.
	lines, hits = b.SubstituteAll("alpha", "Ω", true, true)
	if lines != 2 || hits != 2 {
		t.Fatalf("global sub = %d/%d", lines, hits)
	}
	if got := b.Text(); strings.Contains(got, "alpha") {
		t.Fatalf("alpha survived: %q", got)
	}
	if !strings.Contains(b.Text(), "Ω") {
		t.Fatalf("replacement missing: %q", b.Text())
	}
	if !b.Dirty() {
		t.Fatal("substitution not dirty")
	}
}

func TestSubstituteNotFound(t *testing.T) {
	b := New("one\ntwo\n")
	lines, hits := b.SubstituteAll("zzz", "x", true, false)
	if lines != 0 || hits != 0 {
		t.Fatalf("sub = %d/%d, want 0/0", lines, hits)
	}
	if b.Dirty() {
		t.Fatal("no-op substitution dirtied the buffer")
	}
}

func TestExSubstitute(t *testing.T) {
	e := NewEditor("cat sat\ncat\n")
	// :s on the cursor line (first hit only).
	e.Key(":")
	for _, k := range []string{"s","/","c","a","t","/","d","o","g"} { e.Key(k) }
	e.Key("enter")
	if got := e.Buffer().Line(0); got != "dog sat" {
		t.Fatalf("cursor :s = %q", got)
	}
	if got := e.Buffer().Line(1); got != "cat" {
		t.Fatalf(":s leaked across lines: %q", got)
	}
	// Whole-buffer form; without g the second cat on a line survives.
	e.Buffer().SetCursor(Pos{Line: 0, Col: 0})
	e.Key(":")
	for _, k := range []string{"%",  "s","/","c","a","t","/","d","o","g","/","g"} { e.Key(k) }
	e.Key("enter")
	if got := e.Buffer().Text(); strings.Contains(got, "cat") {
		t.Fatalf("%%s left a cat: %q", got)
	}
	// Not-a-substitute falls through to the unknown-command path.
	e.Key(":")
	for _, k := range []string{"z","z","z"} { e.Key(k) }
	e.Key("enter")
	if !strings.Contains(e.Message(), "not an editor command") {
		t.Fatalf("unknown ex message = %q", e.Message())
	}
}
