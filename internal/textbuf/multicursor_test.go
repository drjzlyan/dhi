package textbuf

import (
	"reflect"
	"testing"
)

func mcEditor(text string, at Pos) *Editor {
	e := NewEditor(text)
	e.buf.SetCursor(at)
	return e
}

func feedKeys(e *Editor, keys ...string) {
	for _, k := range keys {
		e.Key(k)
	}
}

func allCursors(e *Editor) []Pos { return append([]Pos{e.buf.Cursor()}, e.Cursors()...) }

func TestMultiCursorCommands(t *testing.T) {
	tests := []struct {
		name string
		in   string
		at   Pos
		keys []string
		want string
	}{
		{"gb twice then ciw", "foo bar foo baz foo", Pos{0, 1},
			[]string{"gb", "gb", "c", "i", "w", "X", "esc"}, "X bar X baz X"},
		{"alt+n is gb", "foo bar foo", Pos{0, 0},
			[]string{"alt+n", "c", "i", "w", "X", "esc"}, "X bar X"},
		{"offset kept: cursor mid word", "abcd abcd", Pos{0, 2},
			[]string{"gb", "x"}, "abd abd"},
		{"insert typing at every cursor", "a b\na b\na b", Pos{0, 0},
			[]string{"alt+down", "alt+down", "A", "!", "?", "esc"}, "a b!?\na b!?\na b!?"},
		{"I and typing", "  a\n  b\n  c", Pos{0, 2},
			[]string{"alt+down", "alt+down", "I", "-", "esc"}, "  -a\n  -b\n  -c"},
		{"backspace", "abc\nabc", Pos{0, 2},
			[]string{"alt+down", "a", "backspace", "backspace", "esc"}, "a\na"},
		{"delete key in insert", "abc\nabc", Pos{0, 0},
			[]string{"alt+down", "i", "delete", "esc"}, "bc\nbc"},
		{"enter splits every line and keeps counts right", "a\nb\nc", Pos{0, 0},
			[]string{"alt+down", "alt+down", "A", "enter", "x", "esc"}, "a\nx\nb\nx\nc\nx"},
		{"o opens a line under each", "a\nb\nc", Pos{0, 0},
			[]string{"alt+down", "alt+down", "o", "n", "esc"}, "a\nn\nb\nn\nc\nn"},
		{"O opens above each", "a\nb", Pos{0, 0},
			[]string{"alt+down", "O", "n", "esc"}, "n\na\nn\nb"},
		{"dd on lines changes counts above others", "a\nb\nc\nd", Pos{0, 0},
			[]string{"alt+down", "alt+down", "d", "d"}, "d"},
		{"dd on the first and last line", "a\nb\nc\nd", Pos{0, 0},
			[]string{"alt+down", "alt+down", "alt+down", "d", "d"}, ""},
		{"x", "ab\ncd\nef", Pos{0, 0},
			[]string{"alt+down", "alt+down", "x"}, "b\nd\nf"},
		{"dw", "a b\nc d", Pos{0, 0},
			[]string{"alt+down", "d", "w"}, "b\nd"},
		{"d$", "ab cd\nef gh", Pos{0, 3},
			[]string{"alt+down", "d", "$"}, "ab \nef "},
		{"C then insert", "ab cd\nef gh", Pos{0, 3},
			[]string{"alt+down", "C", "X", "esc"}, "ab X\nef X"},
		{"d0", "abc\nabc", Pos{0, 2},
			[]string{"alt+down", "d", "0"}, "c\nc"},
		{"db", "aa bb\ncc dd", Pos{0, 4},
			[]string{"alt+down", "d", "b"}, "aa b\ncc d"},
		{"de", "aa bb\ncc dd", Pos{0, 0},
			[]string{"alt+down", "d", "e"}, " bb\n dd"},
		{"cw", "aa bb\ncc dd", Pos{0, 0},
			[]string{"alt+down", "c", "w", "Z", "esc"}, "Z bb\nZ dd"},
		{"motion w then x", "aa bb\ncc dd", Pos{0, 0},
			[]string{"alt+down", "w", "x"}, "aa b\ncc d"},
		{"motions 0 w", "aa bb\ncc dd", Pos{0, 3},
			[]string{"alt+down", "0", "x", "w", "x"}, "a b\nc d"},
		{"motion b", "aa bb\ncc dd", Pos{0, 4},
			[]string{"alt+down", "b", "x"}, "aa b\ncc d"},
		{"motion $ then insert", "ab\ncd", Pos{0, 0},
			[]string{"alt+down", "$", "i", "!", "esc"}, "ab!\ncd!"},
		{"count d2w", "a b c\nd e f", Pos{0, 0},
			[]string{"alt+down", "d", "2", "w"}, "c\nf"},
		{"text object di(", "f(a)\ng(b)", Pos{0, 2},
			[]string{"alt+down", "d", "i", "("}, "f()\ng()"},
		{"text object ciw", "foo bar\nbaz qux", Pos{0, 5},
			[]string{"alt+down", "c", "i", "w", "_", "esc"}, "foo _\nbaz _"},
		{"yank then paste", "ab\ncd", Pos{0, 0},
			[]string{"alt+down", "y", "l", "p"}, "aab\nccd"},
		{"dot repeat", "abc\ndef", Pos{0, 0},
			[]string{"alt+down", "x", "."}, "c\nf"},
		{"paste text", "a\nb", Pos{0, 0},
			[]string{"alt+down", "A", "paste:XY", "esc"}, "aXY\nbXY"},
		{"autopair types at every cursor", "a\nb", Pos{0, 0},
			[]string{"alt+down", "A", "(", "esc"}, "a()\nb()"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := mcEditor(tc.in, tc.at)
			feedKeys(e, tc.keys...)
			if got := e.buf.Text(); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAddNextOccurrence(t *testing.T) {
	t.Run("whole words only", func(t *testing.T) {
		e := mcEditor("foo food foo", Pos{0, 0})
		e.Key("gb")
		if got := e.Cursors(); !reflect.DeepEqual(got, []Pos{{0, 9}}) {
			t.Errorf("cursors = %v", got)
		}
	})
	t.Run("wraps and stops", func(t *testing.T) {
		e := mcEditor("x foo\nfoo y\nfoo", Pos{1, 0})
		feedKeys(e, "gb", "gb")
		if e.CursorCount() != 3 {
			t.Fatalf("count = %d", e.CursorCount())
		}
		if got := e.Cursors(); !reflect.DeepEqual(got, []Pos{{0, 2}, {2, 0}}) {
			t.Errorf("cursors = %v", got)
		}
		e.Key("gb")
		if e.Message() != "no more occurrences" || e.CursorCount() != 3 {
			t.Errorf("message %q count %d", e.Message(), e.CursorCount())
		}
	})
	t.Run("not on a word", func(t *testing.T) {
		e := mcEditor("a  b", Pos{0, 1})
		e.Key("gb")
		if e.CursorCount() != 1 || e.Message() == "" {
			t.Errorf("count %d message %q", e.CursorCount(), e.Message())
		}
	})
	t.Run("only one occurrence", func(t *testing.T) {
		e := mcEditor("solo", Pos{0, 0})
		e.Key("gb")
		if e.CursorCount() != 1 || e.mc != nil {
			t.Errorf("count %d mc %v", e.CursorCount(), e.mc)
		}
	})
	t.Run("visual selection is the needle, plain text", func(t *testing.T) {
		e := mcEditor("ab abab ab", Pos{0, 0})
		feedKeys(e, "v", "l", "gb")
		if e.Mode() != ModeNormal {
			t.Errorf("mode = %v", e.Mode())
		}
		if got := allCursors(e); !reflect.DeepEqual(got, []Pos{{0, 0}, {0, 3}}) {
			t.Errorf("cursors = %v", got)
		}
		e.Key("gb")
		if got := allCursors(e); !reflect.DeepEqual(got, []Pos{{0, 0}, {0, 3}, {0, 5}}) {
			t.Errorf("cursors = %v", got)
		}
	})
	t.Run("skip", func(t *testing.T) {
		e := mcEditor("foo foo foo foo", Pos{0, 0})
		feedKeys(e, "gb", "gB")
		if got := e.Cursors(); !reflect.DeepEqual(got, []Pos{{0, 8}}) {
			t.Errorf("after skip cursors = %v", got)
		}
		feedKeys(e, "gb", "gb") // 12, then only the skipped one is left
		if got := e.Cursors(); !reflect.DeepEqual(got, []Pos{{0, 8}, {0, 12}}) {
			t.Errorf("cursors = %v", got)
		}
	})
	t.Run("skip with nothing added", func(t *testing.T) {
		e := mcEditor("foo", Pos{0, 0})
		e.Key("gB")
		if e.CursorCount() != 1 || e.Message() == "" {
			t.Error("skip invented a cursor")
		}
	})
	t.Run("an edit forgets the needle", func(t *testing.T) {
		e := mcEditor("foo foo\nbar bar", Pos{0, 0})
		feedKeys(e, "gb", "j", "gb")
		// the j moved both cursors to line 1; gb now needs a word there
		if e.mc == nil || e.mc.needle != "foo" {
			t.Fatalf("mc = %+v", e.mc)
		}
	})
}

func TestVerticalCursors(t *testing.T) {
	e := mcEditor("abc\nd\nefg\nhij", Pos{0, 2})
	feedKeys(e, "alt+down", "alt+down", "alt+down")
	if got := e.Cursors(); !reflect.DeepEqual(got, []Pos{{1, 0}, {2, 2}, {3, 2}}) {
		t.Errorf("cursors = %v", got)
	}
	e.Key("alt+down")
	if e.CursorCount() != 4 || e.Message() != "no line below" {
		t.Errorf("count %d message %q", e.CursorCount(), e.Message())
	}
	e.Key("alt+up") // the primary sits on line 0: nothing above the topmost
	if e.Message() != "no line above" {
		t.Errorf("message %q", e.Message())
	}
	x := mcEditor("a\nb", Pos{0, 0})
	feedKeys(x, ":", "c", "u", "r", "s", "o", "r", " ", "b", "e", "l", "o", "w", "enter")
	if x.CursorCount() != 2 {
		t.Errorf(":cursor below count = %d (%q)", x.CursorCount(), x.Message())
	}
	feedKeys(x, ":", "c", "u", "r", "s", "o", "r", " ", "o", "f", "f", "enter")
	if x.CursorCount() != 1 {
		t.Errorf(":cursor off count = %d", x.CursorCount())
	}
}

func TestMultiCursorEscUndoMerge(t *testing.T) {
	t.Run("esc collapses to the primary", func(t *testing.T) {
		e := mcEditor("a\nb\nc", Pos{1, 0})
		feedKeys(e, "alt+down", "alt+up")
		if e.CursorCount() != 3 {
			t.Fatalf("count = %d", e.CursorCount())
		}
		e.Key("esc")
		if e.CursorCount() != 1 || e.buf.Cursor() != (Pos{1, 0}) {
			t.Errorf("count %d cursor %v", e.CursorCount(), e.buf.Cursor())
		}
	})
	t.Run("esc with a pending operator cancels it first", func(t *testing.T) {
		e := mcEditor("a\nb", Pos{0, 0})
		feedKeys(e, "alt+down", "d", "esc")
		if e.CursorCount() != 2 || e.pendingOp != opNone {
			t.Errorf("count %d pending %v", e.CursorCount(), e.pendingOp)
		}
	})
	t.Run("one command is one undo step", func(t *testing.T) {
		e := mcEditor("ab\ncd\nef", Pos{0, 0})
		feedKeys(e, "alt+down", "alt+down", "x")
		if e.buf.Text() != "b\nd\nf" {
			t.Fatalf("text = %q", e.buf.Text())
		}
		e.Key("u")
		if e.buf.Text() != "ab\ncd\nef" || e.CursorCount() != 1 {
			t.Errorf("after undo %q, %d cursors", e.buf.Text(), e.CursorCount())
		}
	})
	t.Run("an operator command is one undo step", func(t *testing.T) {
		e := mcEditor("a b\nc d\ne f", Pos{0, 0})
		feedKeys(e, "alt+down", "alt+down", "d", "w", "u")
		if e.buf.Text() != "a b\nc d\ne f" {
			t.Errorf("after undo %q", e.buf.Text())
		}
	})
	t.Run("an insert session is one undo step", func(t *testing.T) {
		e := mcEditor("a\nb", Pos{0, 0})
		feedKeys(e, "alt+down", "A", "x", "y", "enter", "z", "esc", "u")
		if e.buf.Text() != "a\nb" {
			t.Errorf("after undo %q", e.buf.Text())
		}
	})
	t.Run("cursors on the same line merge on dd", func(t *testing.T) {
		e := mcEditor("ab ab\nef", Pos{0, 0})
		feedKeys(e, "gb", "d", "d")
		if e.buf.Text() != "ef" || e.CursorCount() != 1 {
			t.Errorf("text %q, %d cursors", e.buf.Text(), e.CursorCount())
		}
	})
	t.Run("adjacent deletes merge", func(t *testing.T) {
		e := mcEditor("aaaa", Pos{0, 1})
		e.mc = &multi{extra: []mcur{{pos: Pos{0, 2}, want: 2}}}
		e.Key("x")
		if e.buf.Text() != "aa" || e.CursorCount() != 1 {
			t.Errorf("text %q, %d cursors", e.buf.Text(), e.CursorCount())
		}
	})
	t.Run("a delete over another cursor swallows it", func(t *testing.T) {
		e := mcEditor("one two three", Pos{0, 8})
		e.mc = &multi{extra: []mcur{{pos: Pos{0, 4}, want: 4}}}
		feedKeys(e, "d", "b")
		if e.CursorCount() != 1 || e.buf.Text() != "one three" {
			t.Errorf("%d cursors, text %q", e.CursorCount(), e.buf.Text())
		}
	})
	t.Run("the primary survives being swallowed", func(t *testing.T) {
		e := mcEditor("ab\ncd", Pos{0, 0})
		e.mc = &multi{extra: []mcur{{pos: Pos{0, 1}, want: 1}}}
		feedKeys(e, "d", "d")
		if e.buf.Text() != "cd" || e.CursorCount() != 1 || e.buf.Cursor().Line != 0 {
			t.Errorf("text %q cursors %d at %v", e.buf.Text(), e.CursorCount(), e.buf.Cursor())
		}
	})
	t.Run("search, ex and visual end multi-cursor mode", func(t *testing.T) {
		for _, k := range []string{"v", ":", "/", "u"} {
			e := mcEditor("a\nb", Pos{0, 0})
			feedKeys(e, "alt+down", k)
			if e.CursorCount() != 1 {
				t.Errorf("%q kept %d cursors", k, e.CursorCount())
			}
		}
	})
}

func TestMultiCursorFollowsEdits(t *testing.T) {
	// Each Enter adds a line above the cursors below it, which must move down.
	e := mcEditor("a\nb\nc", Pos{0, 0})
	feedKeys(e, "alt+down", "alt+down", "A", "enter")
	want := []Pos{{1, 0}, {3, 0}, {5, 0}}
	if got := allCursors(e); !reflect.DeepEqual(got, want) {
		t.Errorf("cursors = %v, want %v (text %q)", got, want, e.buf.Text())
	}
	if e.buf.Cursor() != (Pos{1, 0}) {
		t.Errorf("primary = %v", e.buf.Cursor())
	}
	// Joining lines (backspace at a line start) moves later cursors up.
	j := mcEditor("a\nb\nc", Pos{0, 0})
	feedKeys(j, "alt+down", "alt+down", "I", "backspace")
	// line 0 has nothing to join with; lines 1 and 2 join upward
	if j.buf.Text() != "abc" {
		t.Errorf("text = %q", j.buf.Text())
	}
	if got := allCursors(j); !reflect.DeepEqual(got, []Pos{{0, 0}, {0, 1}, {0, 2}}) {
		t.Errorf("cursors after joins = %v", got)
	}
}

func TestSingleCursorPathUntouched(t *testing.T) {
	// With one line the vertical keys have nowhere to go: still one cursor.
	e := mcEditor("a b", Pos{0, 0})
	feedKeys(e, "alt+up", "alt+down", "x")
	if e.buf.Text() != " b" || e.CursorCount() != 1 || e.mc != nil {
		t.Errorf("text %q cursors %d", e.buf.Text(), e.CursorCount())
	}
}
