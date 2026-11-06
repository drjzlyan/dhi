package textbuf

import "testing"

// ladderSteps expands n times from the cursor and reports each rung's name
// and selected text.
func ladderSteps(e *Editor, n int) (names, texts []string) {
	for i := 0; i < n; i++ {
		name := e.StructuralExpand()
		if name == "" {
			break
		}
		sel, _, _, _ := e.Selection()
		names = append(names, name)
		texts = append(texts, sel)
	}
	return names, texts
}

func TestStructuralLadder(t *testing.T) {
	src := "func f() {\n\tx := g(\"ab cd\", 1)\n\n\ty := 2\n}"
	e := NewEditor(src)
	e.Buffer().SetCursor(Pos{1, 9}) // on the "a" of "ab cd"
	names, texts := ladderSteps(e, 20)
	want := []struct{ name, text string }{
		{"word", "ab"},
		{"inside quotes", "ab cd"},
		{"quotes", "\"ab cd\""},
		{"inside brackets", "\"ab cd\", 1"},
		{"brackets", "(\"ab cd\", 1)"},
		{"line text", "x := g(\"ab cd\", 1)"},
		{"line", "\tx := g(\"ab cd\", 1)"},
		{"inside brackets", "\n\tx := g(\"ab cd\", 1)\n\n\ty := 2"}, // smaller than the paragraph
		{"brackets", "{\n\tx := g(\"ab cd\", 1)\n\n\ty := 2\n}"},
		{"line", src}, // the lines the selection touches
	}
	if len(texts) != len(want) {
		t.Fatalf("ladder = %v %q", names, texts)
	}
	for i, w := range want {
		if names[i] != w.name || texts[i] != w.text {
			t.Errorf("step %d = %s %q, want %s %q", i, names[i], texts[i], w.name, w.text)
		}
	}
	if e.StructuralExpand() != "" {
		t.Error("expanded past the whole buffer")
	}
}

func TestStructuralLadderBufferRung(t *testing.T) {
	e := NewEditor("a\n\nb c")
	e.Buffer().SetCursor(Pos{2, 2})
	names, texts := ladderSteps(e, 10)
	if names[len(names)-1] != "buffer" || texts[len(texts)-1] != "a\n\nb c" {
		t.Fatalf("%v %q", names, texts)
	}
}

func TestStructuralLadderRungNames(t *testing.T) {
	e := NewEditor("a [b, (c)] d")
	e.Buffer().SetCursor(Pos{0, 7}) // on c: the word is the character already selected
	names, texts := ladderSteps(e, 3)
	if len(names) != 3 || names[0] != "brackets" || texts[0] != "(c)" ||
		names[1] != "inside brackets" || texts[1] != "b, (c)" || names[2] != "brackets" || texts[2] != "[b, (c)]" {
		t.Fatalf("names %v texts %q", names, texts)
	}
}

func TestShrinkWalksBack(t *testing.T) {
	e := NewEditor("f(a, (b))")
	e.Buffer().SetCursor(Pos{0, 6})
	if e.ShrinkSelection() {
		t.Fatal("shrunk with nothing expanded")
	}
	var seen []string
	for i := 0; i < 3; i++ {
		e.StructuralExpand()
		s, _, _, _ := e.Selection()
		seen = append(seen, s)
	}
	for i := 1; i >= 0; i-- {
		if !e.ShrinkSelection() {
			t.Fatalf("shrink %d refused", i)
		}
		if s, _, _, _ := e.Selection(); s != seen[i] {
			t.Errorf("shrunk to %q, want %q", s, seen[i])
		}
	}
	if !e.ShrinkSelection() || e.Mode() != ModeNormal {
		t.Fatalf("last shrink should leave visual mode (mode %v)", e.Mode())
	}
	if c := e.Buffer().Cursor(); c != (Pos{0, 6}) {
		t.Errorf("cursor after full shrink = %v", c)
	}
	if e.ShrinkSelection() {
		t.Error("shrunk below the start")
	}
}

func TestShrinkChainBrokenByOtherMoves(t *testing.T) {
	e := NewEditor("f(a, (b))")
	e.Buffer().SetCursor(Pos{0, 6})
	e.StructuralExpand()
	e.StructuralExpand()
	feed(e, "l") // the user moved the selection themselves
	if e.ShrinkSelection() {
		t.Fatal("shrink restored a selection the user had left")
	}
	e.StructuralExpand() // a new chain starts from here
	if !e.ShrinkSelection() {
		t.Fatal("new chain should shrink")
	}
}

func TestExpandFromVisualSelectionAndApply(t *testing.T) {
	e := NewEditor("one two three")
	feed(e, "w", "v", "e") // "two"
	e.StructuralExpand()
	if s, _, _, _ := e.Selection(); s != "one two three" {
		t.Fatalf("expanded to %q", s) // word is not larger than the selection: line text next
	}
	e = NewEditor("x (a b) y")
	e.ApplySelection(Pos{0, 3}, Pos{0, 6}) // what a language server would pick
	if s, _, _, _ := e.Selection(); s != "a b" || e.Mode() != ModeVisual {
		t.Fatalf("applied %q mode %v", s, e.Mode())
	}
	if !e.ShrinkSelection() || e.Mode() != ModeNormal {
		t.Error("shrink after apply should return to normal mode")
	}
}

func TestLadderEdges(t *testing.T) {
	e := NewEditor("")
	if e.StructuralExpand() != "" {
		t.Error("an empty buffer has nothing to expand")
	}
	e = NewEditor("a\n\nb")
	e.Buffer().SetCursor(Pos{0, 0})
	names, _ := ladderSteps(e, 10)
	if len(names) == 0 || names[len(names)-1] != "buffer" {
		t.Errorf("names = %v", names)
	}
	e = NewEditor("ab")
	feed(e, "ctrl+v")
	if e.StructuralExpand() != "" {
		t.Error("a block selection cannot expand")
	}
}

func TestChangeLinewiseBeforeBlankKeepsSeparator(t *testing.T) {
	e := NewEditor("a\n\nc")
	feed(e, "c", "c", "x", "esc")
	if got := text(e); got != "x\n\nc" {
		t.Fatalf("cc: %q", got)
	}
	e = NewEditor("a\nb\n\nc")
	feed(e, "d", "j")
	if got := text(e); got != "\nc" {
		t.Fatalf("dj before a blank line: %q", got)
	}
}
