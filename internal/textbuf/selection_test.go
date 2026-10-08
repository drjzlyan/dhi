package textbuf

import "testing"

func keys(e *Editor, ks ...string) {
	for _, k := range ks {
		e.Key(k)
	}
}

func TestSelectionSingleAndMultiLine(t *testing.T) {
	e := NewEditor("alpha beta\ngamma\ndelta")
	if _, _, _, ok := e.Selection(); ok {
		t.Fatal("no selection outside visual mode")
	}
	keys(e, "v", "l", "l") // "alp" (cursor char included)
	text, from, to, ok := e.Selection()
	if !ok || text != "alp" || from != (Pos{0, 0}) || to != (Pos{0, 3}) {
		t.Fatalf("single-line = %q %v %v ok=%v", text, from, to, ok)
	}
	keys(e, "j", "j")
	text, _, _, _ = e.Selection()
	if text != "alpha beta\ngamma\ndel" {
		t.Fatalf("multi-line = %q", text)
	}
}

func TestColonFromVisualKeepsSelection(t *testing.T) {
	e := NewEditor("hello world")
	keys(e, "v", "l", "l", "l", "l", ":")
	if e.Mode() != ModeCommand {
		t.Fatalf("mode = %v, want command", e.Mode())
	}
	sel, ok := e.TakeSelection()
	if !ok || sel.Text != "hello" {
		t.Fatalf("captured = %+v ok=%v", sel, ok)
	}
	if _, ok := e.TakeSelection(); ok {
		t.Fatal("selection must clear after Take")
	}
}
