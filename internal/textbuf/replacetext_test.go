package textbuf

import "testing"

func TestReplaceTextUnique(t *testing.T) {
	b := New("foo bar baz\n")
	n, err := b.ReplaceText("bar", "qux", false)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if b.Text() != "foo qux baz\n" {
		t.Fatalf("text=%q", b.Text())
	}
	if !b.Dirty() {
		t.Fatal("replace must mark dirty")
	}
}

func TestReplaceTextUndoIsOneStep(t *testing.T) {
	b := New("a b c d\n")
	if _, err := b.ReplaceText(" ", "_", true); err != nil {
		t.Fatal(err)
	}
	if b.Text() != "a_b_c_d\n" {
		t.Fatalf("text=%q", b.Text())
	}
	if !b.Undo() {
		t.Fatal("undo returned false")
	}
	if b.Text() != "a b c d\n" {
		t.Fatalf("after undo=%q", b.Text())
	}
}

func TestReplaceTextNotFoundRefuses(t *testing.T) {
	b := New("alpha\n")
	if _, err := b.ReplaceText("zzz", "x", false); err == nil {
		t.Fatal("missing text must refuse")
	}
}

func TestReplaceTextAmbiguousRefusesUnlessAll(t *testing.T) {
	b := New("x x x\n")
	if _, err := b.ReplaceText("x", "y", false); err == nil {
		t.Fatal("ambiguous replace must refuse")
	}
	n, err := b.ReplaceText("x", "y", true)
	if err != nil || n != 3 {
		t.Fatalf("all n=%d err=%v", n, err)
	}
	if b.Text() != "y y y\n" {
		t.Fatalf("text=%q", b.Text())
	}
}

func TestReplaceTextEmptyOldRefuses(t *testing.T) {
	b := New("abc\n")
	if _, err := b.ReplaceText("", "x", true); err == nil {
		t.Fatal("empty old must refuse")
	}
}
