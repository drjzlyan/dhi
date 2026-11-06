package textbuf

import (
	"strings"
	"testing"
)

func run(e *Editor, keys string) {
	for _, k := range strings.Fields(keys) {
		e.Key(k)
	}
}

func TestBlockOps(t *testing.T) {
	const abc = "abcdef\nghijkl\nmnopqr\nstuvwx"
	cases := []struct {
		name, in, keys, want string
	}{
		{"delete", abc, "l ctrl+v j l d", "adef\ngjkl\nmnopqr\nstuvwx"},
		{"x", abc, "l ctrl+v j l x", "adef\ngjkl\nmnopqr\nstuvwx"},
		{"delete upward corner", abc, "j l l ctrl+v k h d", "adef\ngjkl\nmnopqr\nstuvwx"},
		{"dollar deletes to each end", "abcdef\nab\nabcd", "l l ctrl+v j j $ d", "ab\nab\nab"},
		{"dollar then j keeps eol", "abcdef\nab\nabcd", "l l ctrl+v $ j j d", "ab\nab\nab"},
		{"short line untouched", "abcdef\na\nabcdef", "l l ctrl+v j j l d", "abef\na\nabef"},
		{"yank paste after", abc, "ctrl+v j l y $ p", "abcdefab\nghijklgh\nmnopqr\nstuvwx"},
		{"paste before", abc, "ctrl+v j l y l P", "aabbcdef\ngghhijkl\nmnopqr\nstuvwx"},
		{"paste pads short lines", "ab\nc\nxyzw", "ctrl+v j l y j j $ p", "ab\nc\nxyzwab\n    c"},
		{"paste mid-line pads block width", "ab\nc\nxyzw\n1234", "ctrl+v j l y j j p", "ab\nc\nxabyzw\n1c 234"},
		{"paste count", "ab\ncd", "ctrl+v j y $ 2 p", "abaa\ncdcc"},
		{"change replicates", abc, "l ctrl+v j l c X Y esc", "aXYdef\ngXYjkl\nmnopqr\nstuvwx"},
		{"change skips short", "abcdef\na\nabcdef", "l l ctrl+v j j l c Z esc", "abZef\na\nabZef"},
		{"I inserts every line", abc, "l ctrl+v j j I - esc", "a-bcdef\ng-hijkl\nm-nopqr\nstuvwx"},
		{"I skips short line", "abcdef\na\nabcdef", "l l ctrl+v j j I - esc", "ab-cdef\na\nab-cdef"},
		{"A appends after block", abc, "l ctrl+v j l A | esc", "abc|def\nghi|jkl\nmnopqr\nstuvwx"},
		{"A pads short lines", "abcdef\na\nabcdef", "l l l ctrl+v j j A | esc", "abcd|ef\na   |\nabcd|ef"},
		{"A with dollar", "abc\nab\nabcd", "ctrl+v j j $ A ! esc", "abc!\nab!\nabcd!"},
		{"replace", abc, "l ctrl+v j l r -", "a--def\ng--jkl\nmnopqr\nstuvwx"},
		{"replace skips past end", "abcdef\na\nabcdef", "l l ctrl+v j j l r -", "ab--ef\na\nab--ef"},
		{"shift right", abc, "l ctrl+v j >", "a\tbcdef\ng\thijkl\nmnopqr\nstuvwx"},
		{"shift left", "a  bc\ng  hi", "l ctrl+v j <", "abc\nghi"},
		{"upper", abc, "l ctrl+v j l U", "aBCdef\ngHIjkl\nmnopqr\nstuvwx"},
		{"lower", "ABCD\nEFGH", "l ctrl+v j l u", "AbcD\nEfgH"},
		{"toggle", "aBcD\nEfGh", "l ctrl+v j l ~", "abCD\nEFgh"},
		{"D deletes to end", abc, "l l ctrl+v j D", "ab\ngh\nmnopqr\nstuvwx"},
		{"C changes to end", abc, "l l ctrl+v j C Z esc", "abZ\nghZ\nmnopqr\nstuvwx"},
		{"O swaps columns", abc, "l ctrl+v j l O d", "adef\ngjkl\nmnopqr\nstuvwx"},
		{"o swaps corners", abc, "l ctrl+v j l o d", "adef\ngjkl\nmnopqr\nstuvwx"},
		{"esc cancels", abc, "ctrl+v j l esc", abc},
		{"ctrl+v again leaves", abc, "ctrl+v j ctrl+v x", "abcdef\nhijkl\nmnopqr\nstuvwx"},
		{"find extends", "abcdef\nabcdef", "ctrl+v j f d d", "ef\nef"},
	}
	for _, c := range cases {
		e := NewEditor(c.in)
		e.SetIndent("\t")
		run(e, c.keys)
		if got := e.Buffer().Text(); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestBlockSliceTabsAndWide(t *testing.T) {
	e := NewEditor("")
	cases := []struct {
		line    string
		l, r    int
		b, m, a string
	}{
		{"\tabcd", 2, 6, "  ", "  ab", "cd"},
		{"a\tb", 1, 3, "a", "  ", " b"},
		{"a世b", 2, 4, "a ", " b", ""},
		{"a世b", 1, 3, "a", "世", "b"},
		{"ab", 5, 7, "ab", "", ""},
		{"abc", 1, blockInf, "a", "bc", ""},
	}
	for _, c := range cases {
		b, m, a := e.blockSlice(c.line, c.l, c.r)
		if b != c.b || m != c.m || a != c.a {
			t.Errorf("%q [%d,%d): %q %q %q, want %q %q %q", c.line, c.l, c.r, b, m, a, c.b, c.m, c.a)
		}
	}
}

func TestBlockByDisplayColumn(t *testing.T) {
	// the tab is 4 cells: the block over its second cell cuts it into spaces
	e := NewEditor("\tab\nxxxxab")
	e.SetTabWidth(4)
	run(e, "ctrl+v j l d") // anchor tab (cells 0-3), cursor on cell 1: block cells 0..3
	if got := e.Buffer().Text(); got != "ab\nab" {
		t.Errorf("tab block: %q", got)
	}

	e = NewEditor("a\tb\nxxxxb")
	e.SetTabWidth(4)
	run(e, "l ctrl+v j d") // anchor tab (1..3), cursor 'x' (1): block cells 1..3
	if got := e.Buffer().Text(); got != "ab\nxb" {
		t.Errorf("tab col: %q", got)
	}

	e = NewEditor("a\tb\nxxxxb")
	e.SetTabWidth(8)
	run(e, "l ctrl+v l d")
	if got := e.Buffer().Text(); got != "a\nxxxxb" {
		t.Errorf("tab8: %q", got)
	}

	// wide runes: the block over cell 1 of 世 splits it
	e = NewEditor("a世b\nabcd")
	run(e, "l ctrl+v j y")
	r, _ := e.RegisterText()
	if r != "世\nbc" {
		t.Errorf("wide yank: %q", r)
	}
}

func TestBlockUndoAndDot(t *testing.T) {
	e := NewEditor("abcd\nefgh\nijkl")
	run(e, "l ctrl+v j l c X esc")
	if got := e.Buffer().Text(); got != "aXd\neXh\nijkl" {
		t.Fatalf("change: %q", got)
	}
	e.Key("u")
	if got := e.Buffer().Text(); got != "abcd\nefgh\nijkl" {
		t.Errorf("one undo step: %q", got)
	}

	e = NewEditor("abcd\nefgh\nijkl")
	run(e, "l ctrl+v j I - esc u")
	if got := e.Buffer().Text(); got != "abcd\nefgh\nijkl" {
		t.Errorf("I undo: %q", got)
	}
	run(e, "ctrl+r")
	if got := e.Buffer().Text(); got != "a-bcd\ne-fgh\nijkl" {
		t.Errorf("I redo: %q", got)
	}

	e = NewEditor("abcd\nefgh\nijkl\nmnop")
	run(e, "ctrl+v j d")
	if got := e.Buffer().Text(); got != "bcd\nfgh\nijkl\nmnop" {
		t.Fatalf("d: %q", got)
	}
	run(e, "j j .")
	if got := e.Buffer().Text(); got != "bcd\nfgh\njkl\nnop" {
		t.Errorf("dot repeats same-size block: %q", got)
	}

	e = NewEditor("abcd\nefgh\nijkl\nmnop")
	run(e, "ctrl+v j I - esc j j .")
	if got := e.Buffer().Text(); got != "-abcd\n-efgh\n-ijkl\n-mnop" {
		t.Errorf("dot I: %q", got)
	}
}

func TestBlockModeState(t *testing.T) {
	e := NewEditor("abc\ndef")
	e.Key("ctrl+v")
	if !e.VisualBlock() || e.Mode() != ModeVisual {
		t.Fatal("ctrl+v enters block visual")
	}
	run(e, "l j")
	top, bot, left, right, ok := e.BlockRange()
	if !ok || top != 0 || bot != 1 || left != 0 || right != 2 {
		t.Errorf("range %d %d %d %d %v", top, bot, left, right, ok)
	}
	if text, _, _, _ := e.Selection(); text != "ab\nde" {
		t.Errorf("selection %q", text)
	}
	run(e, "$")
	if _, _, _, r, _ := e.BlockRange(); r < 1000 {
		t.Error("$ extends to line ends")
	}
	run(e, "h")
	if _, _, _, r, _ := e.BlockRange(); r > 1000 {
		t.Error("h ends the $ reach")
	}
	e.Key("v")
	if e.VisualBlock() || e.Mode() != ModeVisual {
		t.Error("v switches to charwise")
	}
	e.Key("ctrl+v")
	if !e.VisualBlock() {
		t.Error("ctrl+v switches back")
	}
	e.Key("esc")
	if e.Mode() != ModeNormal {
		t.Error("esc leaves")
	}
}

func TestBlockPasteNeedsBlockRegister(t *testing.T) {
	e := NewEditor("ab\ncd")
	run(e, "ctrl+v j y")
	run(e, "y y") // a linewise yank replaces the block register
	e.Key("G")
	e.Key("p")
	if got := e.Buffer().Text(); got != "ab\ncd\nab" {
		t.Errorf("linewise paste after block yank: %q", got)
	}
}
