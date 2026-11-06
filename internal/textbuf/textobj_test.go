package textbuf

import "testing"

func TestTextObjectOperators(t *testing.T) {
	tests := []struct {
		name string
		in   string
		at   Pos
		keys []string
		want string
		reg  string // unnamed register after, "" = not checked
	}{
		{"diw", "foo bar baz", Pos{0, 5}, []string{"d", "i", "w"}, "foo  baz", "bar"},
		{"daw trailing", "foo bar baz", Pos{0, 5}, []string{"d", "a", "w"}, "foo baz", "bar "},
		{"daw last word takes leading", "foo bar", Pos{0, 5}, []string{"d", "a", "w"}, "foo", " bar"},
		{"daw on blanks", "foo   bar baz", Pos{0, 4}, []string{"d", "a", "w"}, "foo baz", "   bar"},
		{"d2aw", "a b c d", Pos{0, 0}, []string{"d", "2", "a", "w"}, "c d", "a b "},
		{"2daw", "a b c d", Pos{0, 0}, []string{"2", "d", "a", "w"}, "c d", "a b "},
		{"d3iw word blank word", "foo bar baz", Pos{0, 0}, []string{"d", "3", "i", "w"}, " baz", "foo bar"},
		{"diw punct run", "x.y == z", Pos{0, 4}, []string{"d", "i", "w"}, "x.y  z", "=="},
		{"ciw", "foo bar", Pos{0, 5}, []string{"c", "i", "w", "X", "esc"}, "foo X", ""},
		{"yiw leaves text", "foo bar", Pos{0, 5}, []string{"y", "i", "w"}, "foo bar", "bar"},

		{"di( inner", "f(a, b) x", Pos{0, 3}, []string{"d", "i", "("}, "f() x", "a, b"},
		{"da( outer", "f(a, b) x", Pos{0, 3}, []string{"d", "a", "("}, "f x", "(a, b)"},
		{"dib alias", "f(a, b)", Pos{0, 3}, []string{"d", "i", "b"}, "f()", "a, b"},
		{"di) alias", "f(a, b)", Pos{0, 3}, []string{"d", "i", ")"}, "f()", "a, b"},
		{"on the open bracket", "f(a, b)", Pos{0, 1}, []string{"d", "i", "("}, "f()", "a, b"},
		{"on the close bracket", "f(a, b)", Pos{0, 6}, []string{"d", "i", "("}, "f()", "a, b"},
		{"nested inner", "f(a(b)c)", Pos{0, 4}, []string{"d", "i", "("}, "f(a()c)", "b"},
		{"nested 2i(", "f(a(b)c)", Pos{0, 4}, []string{"d", "2", "i", "("}, "f()", "a(b)c"},
		{"unbalanced skipped", "(a(b)c", Pos{0, 3}, []string{"d", "2", "i", "("}, "(a(b)c", ""},
		{"empty pair", "f()", Pos{0, 1}, []string{"d", "i", "("}, "f()", ""},
		{"ci( on empty pair inserts", "f()", Pos{0, 1}, []string{"c", "i", "(", "x", "esc"}, "f(x)", ""},
		{"di[", "a[1, 2]b", Pos{0, 3}, []string{"d", "i", "["}, "a[]b", "1, 2"},
		{"da[", "a[1, 2]b", Pos{0, 3}, []string{"d", "a", "["}, "ab", "[1, 2]"},
		{"di{ one line", "x {a} y", Pos{0, 3}, []string{"d", "i", "{"}, "x {} y", "a"},
		{"diB alias", "x {a} y", Pos{0, 3}, []string{"d", "i", "B"}, "x {} y", "a"},
		{"di{ multi-line keeps the braces", "if x {\n\ta\n\tb\n}\nz", Pos{1, 1}, []string{"d", "i", "{"}, "if x {\n}\nz", "\ta\n\tb\n"},
		{"da{ multi-line", "if x {\n\ta\n}\nz", Pos{1, 1}, []string{"d", "a", "{"}, "if x \nz", "{\n\ta\n}"},
		{"ci{ multi-line", "f {\n\ta\n}", Pos{1, 1}, []string{"c", "i", "{", "x", "esc"}, "f {\nx}", ""},
		{"yi( multi-line from a later line", "f(a,\n  b,\n  c)", Pos{2, 3}, []string{"y", "i", "("}, "f(a,\n  b,\n  c)", "a,\n  b,\n  c"},
		{"no pair", "foo", Pos{0, 1}, []string{"d", "i", "("}, "foo", ""},

		{"di\" around", `x = "hi there" y`, Pos{0, 7}, []string{"d", "i", "\""}, `x = "" y`, "hi there"},
		{"da\" takes trailing blank", `x = "hi" y`, Pos{0, 6}, []string{"d", "a", "\""}, `x = y`, `"hi" `},
		{"da\" last takes leading blank", `x = "hi"`, Pos{0, 6}, []string{"d", "a", "\""}, `x =`, ` "hi"`},
		{"di\" before the quotes uses the next pair", `x = "hi"`, Pos{0, 0}, []string{"d", "i", "\""}, `x = ""`, "hi"},
		{"di\" on the opening quote", `a "b" "c"`, Pos{0, 2}, []string{"d", "i", "\""}, `a "" "c"`, "b"},
		{"di\" second pair", `a "b" "c"`, Pos{0, 7}, []string{"d", "i", "\""}, `a "b" ""`, "c"},
		{"escaped quote ignored", `"a\"b" z`, Pos{0, 2}, []string{"d", "i", "\""}, `"" z`, `a\"b`},
		{"di' single", `f('a b')`, Pos{0, 4}, []string{"d", "i", "'"}, `f('')`, "a b"},
		{"di` backtick", "x `a` y", Pos{0, 3}, []string{"d", "i", "`"}, "x `` y", "a"},
		{"quote no pair", `x "a`, Pos{0, 3}, []string{"d", "i", "\""}, `x "a`, ""},
		{"ci\"", `s = "old"`, Pos{0, 6}, []string{"c", "i", "\"", "n", "esc"}, `s = "n"`, ""},

		{"dip", "a\nb\n\nc\nd", Pos{0, 0}, []string{"d", "i", "p"}, "\nc\nd", "a\nb\n"},
		{"dap takes the blank after", "a\nb\n\nc\nd", Pos{1, 0}, []string{"d", "a", "p"}, "c\nd", "a\nb\n\n"},
		{"dap last paragraph takes blank before", "a\n\nb\nc", Pos{3, 0}, []string{"d", "a", "p"}, "a", "\nb\nc"},
		{"dip on blank run", "a\n\n\nb", Pos{1, 0}, []string{"d", "i", "p"}, "a\nb", "\n\n"},
		{"d2ip", "a\n\nb", Pos{0, 0}, []string{"d", "2", "i", "p"}, "b", "a\n\n"},
		{"yip", "a\nb\n\nc", Pos{0, 0}, []string{"y", "i", "p"}, "a\nb\n\nc", "a\nb\n"},
		{"cip leaves a line to type on", "a\nb\n\nc", Pos{0, 0}, []string{"c", "i", "p", "x", "esc"}, "x\n\nc", ""},
		{"dip whole buffer", "a\nb", Pos{0, 0}, []string{"d", "i", "p"}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEditor(tc.in)
			e.Buffer().SetCursor(tc.at)
			feed(e, tc.keys...)
			if got := text(e); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
			if tc.reg != "" {
				if reg, _ := e.RegisterText(); reg != tc.reg {
					t.Errorf("register = %q, want %q", reg, tc.reg)
				}
			}
		})
	}
}

func TestTextObjectCommentOperator(t *testing.T) {
	e := goEditor("a\nb\n\nc")
	feed(e, "gc", "i", "p")
	if got := text(e); got != "// a\n// b\n\nc" {
		t.Fatalf("gcip: %q", got)
	}
	e = goEditor("f(\n\ta,\n\tb,\n)")
	e.Buffer().SetCursor(Pos{1, 1})
	feed(e, "gc", "i", "(")
	if got := text(e); got != "f(\n\t// a,\n\t// b,\n)" {
		t.Fatalf("gci(: %q", got)
	}
}

func TestTextObjectDotRepeat(t *testing.T) {
	e := NewEditor("foo bar baz")
	feed(e, "c", "i", "w", "X", "esc", "w", ".")
	if got := text(e); got != "X X baz" {
		t.Fatalf("ciw then . : %q", got)
	}
}

func TestTextObjectUnknownObjectCancels(t *testing.T) {
	e := NewEditor("foo bar")
	feed(e, "d", "i", "z")
	if got := text(e); got != "foo bar" || e.Mode() != ModeNormal {
		t.Fatalf("%q mode %v", got, e.Mode())
	}
	feed(e, "x") // the operator was cancelled, not left pending
	if got := text(e); got != "oo bar" {
		t.Fatalf("after cancel: %q", got)
	}
}

func TestTextObjectVisual(t *testing.T) {
	tests := []struct {
		name string
		in   string
		at   Pos
		keys []string
		want string // the selection
	}{
		{"viw", "foo bar", Pos{0, 5}, []string{"v", "i", "w"}, "bar"},
		{"vaw", "foo bar baz", Pos{0, 5}, []string{"v", "a", "w"}, "bar "},
		{"vi(", "f(a, b)", Pos{0, 3}, []string{"v", "i", "("}, "a, b"},
		{"va(", "f(a, b)", Pos{0, 3}, []string{"v", "a", "("}, "(a, b)"},
		{"vi( twice grows", "f(a(b)c)", Pos{0, 4}, []string{"v", "i", "(", "i", "("}, "a(b)c"},
		{"vi( thrice grows past the pair", "x(a(b)c)", Pos{0, 4}, []string{"v", "i", "(", "i", "(", "a", "("}, "(a(b)c)"},
		{"v2i(", "f(a(b)c)", Pos{0, 4}, []string{"v", "2", "i", "("}, "a(b)c"},
		{"vi\"", `x "hi" y`, Pos{0, 4}, []string{"v", "i", "\""}, "hi"},
		{"va\"", `x "hi" y`, Pos{0, 4}, []string{"v", "a", "\""}, `"hi" `},
		{"vip is linewise", "a\nb\n\nc", Pos{0, 0}, []string{"v", "i", "p"}, "a\nb\n"},
		{"vap", "a\nb\n\nc", Pos{0, 0}, []string{"v", "a", "p"}, "a\nb\n\n"},
		{"vi{ multi-line", "f {\n\ta\n}", Pos{1, 1}, []string{"v", "i", "{"}, "\ta"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEditor(tc.in)
			e.Buffer().SetCursor(tc.at)
			feed(e, tc.keys...)
			if e.Mode() != ModeVisual {
				t.Fatalf("mode = %v", e.Mode())
			}
			if tc.name == "vip is linewise" || tc.name == "vap" {
				feed(e, "d")
				if got, lw := e.RegisterText(); got != tc.want || !lw {
					t.Fatalf("d register = %q lw=%v, want %q", got, lw, tc.want)
				}
				return
			}
			if got, _, _, ok := e.Selection(); !ok || got != tc.want {
				t.Errorf("selection = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTextObjectVisualOperators(t *testing.T) {
	e := NewEditor("f(a, b) x")
	e.Buffer().SetCursor(Pos{0, 3})
	feed(e, "v", "a", "(", "d")
	if got := text(e); got != "f x" {
		t.Fatalf("va(d: %q", got)
	}
	e = NewEditor("foo bar")
	e.Buffer().SetCursor(Pos{0, 5})
	feed(e, "v", "i", "w", "c", "X", "esc")
	if got := text(e); got != "foo X" {
		t.Fatalf("viwc: %q", got)
	}
}

func TestTextObjectUnitHelpers(t *testing.T) {
	b := New("one two")
	if _, _, _, ok := b.textObject(Pos{0, 0}, true, '?', 1); ok {
		t.Error("unknown object resolved")
	}
	if _, _, _, ok := New("").textObject(Pos{0, 0}, true, 'w', 1); ok {
		t.Error("iw on an empty line resolved")
	}
	a, z, lw, ok := New("a\n\nb").textObject(Pos{2, 0}, false, 'p', 1)
	if !ok || !lw || a != (Pos{1, 0}) || z != (Pos{3, 0}) {
		t.Errorf("ap on the last paragraph = %v %v %v %v", a, z, lw, ok)
	}
}
