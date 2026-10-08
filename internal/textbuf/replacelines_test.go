package textbuf

import "testing"

func TestReplaceLinesIsOneUndoStep(t *testing.T) {
	b := New("a\nb\nc")
	if n := b.ReplaceLines(map[int]string{0: "A", 2: "C", 9: "ignored"}); n != 2 {
		t.Fatalf("changed = %d, want 2", n)
	}
	if b.Text() != "A\nb\nC" || !b.Dirty() {
		t.Fatalf("text = %q dirty=%v", b.Text(), b.Dirty())
	}
	if !b.Undo() || b.Text() != "a\nb\nc" {
		t.Fatalf("one undo must restore every line: %q", b.Text())
	}
	if b.ReplaceLines(map[int]string{1: "b"}) != 0 {
		t.Fatal("an identical line is not a change")
	}
}
