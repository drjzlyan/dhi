package textbuf

import (
	"testing"
)

func mustLines(t *testing.T, b *Buffer) []string {
	t.Helper()
	return b.Lines()
}

func TestNewTrailingNewline(t *testing.T) {
	b := New("a\nb\n")
	if got := b.Text(); got != "a\nb\n" {
		t.Fatalf("New must preserve trailing newline, got %q", got)
	}
	lines := mustLines(t, b)
	if len(lines) != 3 || lines[2] != "" {
		t.Errorf("lines = %q, want [a b \"\"]", lines)
	}
}

func TestInsertAndDeleteBasics(t *testing.T) {
	b := New("hello")
	b.SetCursor(Pos{0, 5})
	b.InsertString(", world")
	if b.Text() != "hello, world" {
		t.Fatalf("text = %q", b.Text())
	}

	b.SetCursor(Pos{0, 0})
	if !b.DeleteForward() || b.Text() != "ello, world" {
		t.Fatalf("delete forward: %q", b.Text())
	}
	b.SetCursor(Pos{0, 1})
	b.DeleteBackspace() // deletes 'e'
	if b.Text() != "llo, world" {
		t.Fatalf("backspace: %q", b.Text())
	}
	if b.Cursor().Col != 0 {
		t.Fatalf("cursor after backspace: %+v", b.Cursor())
	}
}

func TestBreakLineJoins(t *testing.T) {
	b := New("one two")
	b.SetCursor(Pos{0, 4}) // before 't'; space stays on line one
	b.BreakLine()
	if b.Text() != "one \ntwo" {
		t.Fatalf("%q", b.Text())
	}
	if !b.DeleteBackspace() || b.Text() != "one two" {
		t.Fatalf("join via backspace: %q", b.Text())
	}
}

func TestDeleteForwardJoinsAtEOL(t *testing.T) {
	b := New("ab\ncd")
	b.SetCursor(Pos{0, 2})
	b.DeleteForward()
	if b.Text() != "abcd" {
		t.Fatalf("%q", b.Text())
	}
}

func TestUndoRedoRoundTrip(t *testing.T) {
	b := New("start")
	b.SetCursor(Pos{0, 5})
	b.InsertString("+more")
	if !b.Dirty() {
		t.Fatal("not dirty after edit")
	}
	if !b.Undo() || b.Text() != "start" {
		t.Fatalf("undo: %q", b.Text())
	}
	if b.Dirty() {
		t.Error("undo should restore clean flag")
	}
	if !b.Redo() || b.Text() != "start+more" {
		t.Fatalf("redo: %q", b.Text())
	}
	if !b.Undo() || b.Text() != "start" {
		t.Fatal("second undo failed")
	}
	if b.Undo() {
		t.Error("undo beyond history succeeded")
	}
}

func TestMotions(t *testing.T) {
	src := "alpha beta gamma\ntwo\nthree four"
	b := New(src)

	b.SetCursor(Pos{0, 0})
	b.move(motWordFwd, 1)
	if c := b.Cursor(); c.Line != 0 || c.Col != 6 {
		t.Fatalf("w → %+v", c)
	}
	b.move(motWordEnd, 1)
	if b.Cursor().Col != 9 { // end of "beta"
		t.Fatalf("e → %d", b.Cursor().Col)
	}
	b.move(motWordBack, 1)
	if b.Cursor().Col != 6 {
		t.Fatalf("b → %d", b.Cursor().Col)
	}
	b.move(motWordFwd, 2) // wraps: gamma → EOL → next line start
	if c := b.Cursor(); c.Line != 1 || c.Col != 0 {
		t.Fatalf("2w wrap → %+v", c)
	}
	b.move(motUp, 1)
	b.move(motLineEnd, 1)
	if b.Cursor().Col != 16 {
		t.Fatalf("$ → %d", b.Cursor().Col)
	}
	b.move(motDown, 1)
	if c := b.Cursor(); c.Line != 1 || c.Col > len([]rune("two")) {
		t.Fatalf("j sticky → %+v", c)
	}
	b.move(motEOF, 1)
	if b.Cursor().Line != 2 {
		t.Fatalf("G → %+v", b.Cursor())
	}
	b.move(motBOF, 1)
	if b.Cursor() != (Pos{}) {
		t.Fatalf("gg → %+v", b.Cursor())
	}
	b.move(motRight, 3)
	b.move(motWordBack, 2)
	if b.Cursor().Col != 0 {
		t.Fatalf("b → %d", b.Cursor().Col)
	}
}

func TestStickyColumn(t *testing.T) {
	b := New("longline\nx\nanother")
	b.SetCursor(Pos{0, 6})
	b.move(motDown, 1)
	if c := b.Cursor(); c.Col != 1 { // clamped to width of "x"
		t.Fatalf("clamp = %d", c.Col)
	}
	b.move(motDown, 1)
	if c := b.Cursor(); c.Line != 2 || c.Col != 6 {
		t.Fatalf("sticky restore = %+v", c)
	}
}
