package textbuf

import "testing"

func TestSetTextIsOneUndoStep(t *testing.T) {
	b := New("a\nb\nc")
	if b.SetText("a\nb\nc") {
		t.Fatal("identical text reported a change")
	}
	if !b.SetText("x\ny") || b.Text() != "x\ny" || !b.Dirty() {
		t.Fatalf("text=%q dirty=%v", b.Text(), b.Dirty())
	}
	if !b.Undo() || b.Text() != "a\nb\nc" {
		t.Fatalf("one undo should restore: %q", b.Text())
	}
}
