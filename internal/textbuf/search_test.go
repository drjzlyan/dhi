package textbuf

import "testing"

func TestSearchForwardBackwardWrap(t *testing.T) {
	e := NewEditor("alpha\nbeta Alpha\ngamma alpha\n")
	keys(e, "/", "a", "l", "p", "h", "a", "enter")
	if c := e.Buffer().Cursor(); c != (Pos{Line: 1, Col: 5}) {
		t.Fatalf("lower-case query matches any case: cursor %+v", c)
	}
	keys(e, "n")
	if c := e.Buffer().Cursor(); c != (Pos{Line: 2, Col: 6}) {
		t.Fatalf("n: %+v", c)
	}
	keys(e, "n")
	if c := e.Buffer().Cursor(); c != (Pos{Line: 0, Col: 0}) || e.Message() != "search hit BOTTOM, continuing at TOP" {
		t.Fatalf("wrap: %+v %q", c, e.Message())
	}
	keys(e, "N")
	if c := e.Buffer().Cursor(); c != (Pos{Line: 2, Col: 6}) {
		t.Fatalf("N goes back (wrapping): %+v", c)
	}
	keys(e, "/", "A", "l", "enter")
	if c := e.Buffer().Cursor(); c != (Pos{Line: 1, Col: 5}) {
		t.Fatalf("a capital makes the search exact: %+v", c)
	}
	keys(e, "/", "z", "z", "z", "enter")
	if e.Message() != "pattern not found: zzz" {
		t.Fatalf("missing: %q", e.Message())
	}
}

func TestSearchPromptAndStar(t *testing.T) {
	e := NewEditor("foo bar\nbaz foo\n")
	keys(e, "?", "b")
	if e.CommandLine() != "?b" {
		t.Fatalf("prompt: %q", e.CommandLine())
	}
	keys(e, "esc")
	if e.Mode() != ModeNormal {
		t.Fatal("esc leaves the prompt")
	}
	keys(e, "*")
	if c := e.Buffer().Cursor(); c != (Pos{Line: 1, Col: 4}) {
		t.Fatalf("* finds the next foo: %+v", c)
	}
	keys(e, ":")
	if e.CommandLine() != ":" {
		t.Fatalf("ex prompt after a search: %q", e.CommandLine())
	}
}

func TestGGAndPaging(t *testing.T) {
	e := NewEditor("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	e.SetPageSize(4)
	keys(e, "ctrl+d")
	if l := e.Buffer().Cursor().Line; l != 2 {
		t.Fatalf("ctrl+d moves half a page: line %d", l)
	}
	keys(e, "pgdown")
	if l := e.Buffer().Cursor().Line; l != 6 {
		t.Fatalf("pgdown moves a page: line %d", l)
	}
	keys(e, "gg")
	if l := e.Buffer().Cursor().Line; l != 0 {
		t.Fatalf("gg: line %d", l)
	}
}

func TestInsertModeLineKeys(t *testing.T) {
	e := NewEditor("hello brave world")
	keys(e, "A", "ctrl+w")
	if got := e.Buffer().Line(0); got != "hello brave " {
		t.Fatalf("ctrl+w: %q", got)
	}
	keys(e, "alt+left", "ctrl+u")
	if got := e.Buffer().Line(0); got != "brave " {
		t.Fatalf("alt+left then ctrl+u: %q", got)
	}
	keys(e, "end", "paste:new\nline")
	if got := e.Buffer().Line(0) + "|" + e.Buffer().Line(1); got != "brave new|line" {
		t.Fatalf("paste: %q", got)
	}
	keys(e, "esc", "u")
	if got := e.Buffer().Line(0); got != "hello brave world" {
		t.Fatalf("the insert session undoes as one step: %q", got)
	}
}
