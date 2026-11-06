package textbuf

import (
	"reflect"
	"testing"
)

func TestToggleComment(t *testing.T) {
	tests := []struct {
		name  string
		in    []string
		c     Comment
		want  []string
		wantA CommentAction
	}{
		{"comment at min indent", []string{"\ta", "\t\tb", "\tc"}, slashes,
			[]string{"\t// a", "\t// \tb", "\t// c"}, CommentAdded},
		{"blank lines untouched", []string{"a", "", "  ", "b"}, hash,
			[]string{"# a", "", "  ", "# b"}, CommentAdded},
		{"uncomment when all commented", []string{"  // a", "", "  //b", "  //   c"}, slashes,
			[]string{"  a", "", "  b", "    c"}, CommentRemoved},
		{"mixed comments the rest", []string{"// a", "b"}, slashes,
			[]string{"// // a", "// b"}, CommentAdded},
		{"only blank is a no-op", []string{"", "  "}, slashes, []string{"", "  "}, CommentNone},
		{"block pair per line", []string{"  a {", "    x: 1;", ""}, cBlock,
			[]string{"  /* a { */", "  /*   x: 1; */", ""}, CommentAdded},
		{"block uncomment", []string{"  /* a { */", "  /*b*/"}, cBlock,
			[]string{"  a {", "  b"}, CommentRemoved},
		{"html", []string{"<p>x</p>"}, htmlCmt, []string{"<!-- <p>x</p> -->"}, CommentAdded},
		{"space indent common prefix", []string{"    a", "  b"}, hash,
			[]string{"  #   a", "  # b"}, CommentAdded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := append([]string(nil), tc.in...)
			got, act := ToggleComment(tc.in, tc.c)
			if !reflect.DeepEqual(got, tc.want) || act != tc.wantA {
				t.Fatalf("got %q %v, want %q %v", got, act, tc.want, tc.wantA)
			}
			if !reflect.DeepEqual(tc.in, orig) {
				t.Fatalf("input modified: %q", tc.in)
			}
		})
	}
}

func TestToggleCommentRoundTrip(t *testing.T) {
	in := []string{"\tfoo()", "", "\t\tbar()"}
	for _, c := range []Comment{slashes, hash, cBlock, htmlCmt} {
		on, _ := ToggleComment(in, c)
		off, act := ToggleComment(on, c)
		if act != CommentRemoved || !reflect.DeepEqual(off, in) {
			t.Fatalf("%+v: %q -> %q -> %q", c, in, on, off)
		}
	}
}

func TestCommentFor(t *testing.T) {
	for path, want := range map[string]Comment{
		"a/b.go": slashes, "x.PY": hash, "Makefile": hash, "site.css": cBlock,
		"index.html": htmlCmt, "Dockerfile": hash, ".gitignore": hash,
	} {
		if got, ok := CommentFor(path); !ok || got != want {
			t.Errorf("%s: %+v %v", path, got, ok)
		}
	}
	for _, p := range []string{"data.json", "noext", ""} {
		if _, ok := CommentFor(p); ok {
			t.Errorf("%q should have no syntax", p)
		}
	}
}

func goEditor(content string) *Editor {
	e := NewEditor(content)
	e.SetPath("main.go")
	return e
}

func text(e *Editor) string { return e.Buffer().Text() }

func TestGccLineAndCount(t *testing.T) {
	e := goEditor("a\nb\nc\nd")
	feed(e, "gc", "c")
	if got := text(e); got != "// a\nb\nc\nd" {
		t.Fatalf("%q", got)
	}
	feed(e, "gc", "c") // toggles back
	if got := text(e); got != "a\nb\nc\nd" {
		t.Fatalf("%q", got)
	}
	feed(e, "3", "gc", "c")
	if got := text(e); got != "// a\n// b\n// c\nd" {
		t.Fatalf("%q", got)
	}
}

func TestGcMotion(t *testing.T) {
	e := goEditor("a\nb\nc\nd")
	feed(e, "gc", "j") // current + next line
	if got := text(e); got != "// a\n// b\nc\nd" {
		t.Fatalf("gcj: %q", got)
	}
	e = goEditor("a\nb\nc\nd")
	feed(e, "j", "gc", "k")
	if got := text(e); got != "// a\n// b\nc\nd" {
		t.Fatalf("gck: %q", got)
	}
	e = goEditor("a\nb\nc\nd")
	feed(e, "j", "gc", "G")
	if got := text(e); got != "a\n// b\n// c\n// d" {
		t.Fatalf("gcG: %q", got)
	}
	e = goEditor("a\nb\nc\nd")
	feed(e, "2", "gc", "j") // vim: count on the motion's side
	if got := text(e); got != "// a\n// b\n// c\nd" {
		t.Fatalf("2gcj: %q", got)
	}
	e = goEditor("a b\nc")
	feed(e, "gc", "w") // a charwise motion comments its line
	if got := text(e); got != "// a b\nc" {
		t.Fatalf("gcw: %q", got)
	}
}

func TestGcDoubledAndAbandoned(t *testing.T) {
	e := goEditor("a\nb")
	feed(e, "gc", "gc") // gcgc
	if got := text(e); got != "// a\nb" {
		t.Fatalf("gcgc: %q", got)
	}
	e = goEditor("a\nb")
	feed(e, "gc", "esc", "j")
	if got := text(e); got != "a\nb" || e.pendingOp != opNone {
		t.Fatalf("esc: %q", got)
	}
}

func TestGcVisual(t *testing.T) {
	e := goEditor("a\n\n  b\nc")
	feed(e, "V", "j", "j", "gc")
	if got := text(e); got != "// a\n\n//   b\nc" {
		t.Fatalf("%q", got)
	}
	if e.Mode() != ModeNormal {
		t.Fatal("visual gc should leave visual mode")
	}
	feed(e, "V", "j", "j", "gc")
	if got := text(e); got != "a\n\n  b\nc" {
		t.Fatalf("uncomment: %q", got)
	}
}

func TestGcOneUndoStepAndDot(t *testing.T) {
	e := goEditor("a\nb\nc\nd")
	feed(e, "gc", "j", "u")
	if got := text(e); got != "a\nb\nc\nd" {
		t.Fatalf("undo: %q", got)
	}
	feed(e, "gc", "j") // // a, // b
	feed(e, "j", "j", ".")
	if got := text(e); got != "// a\n// b\n// c\n// d" {
		t.Fatalf("dot: %q", got)
	}
	feed(e, "u")
	if got := text(e); got != "// a\n// b\nc\nd" {
		t.Fatalf("dot undone in one step: %q", got)
	}
}

func TestGccDotRepeatsOnNextLine(t *testing.T) {
	e := goEditor("a\nb")
	feed(e, "gc", "c", "j", ".")
	if got := text(e); got != "// a\n// b" {
		t.Fatalf("%q", got)
	}
}

func TestCtrlSlash(t *testing.T) {
	for _, k := range []string{"ctrl+/", "ctrl+_"} {
		e := goEditor("a\nb\nc")
		feed(e, k)
		if got := text(e); got != "// a\nb\nc" {
			t.Fatalf("%s normal: %q", k, got)
		}
		feed(e, "2", k) // two lines from the first: a is commented, b is not
		if got := text(e); got != "// // a\n// b\nc" {
			t.Fatalf("%s count: %q", k, got)
		}
	}
}

func TestCtrlSlashInsertKeepsTyping(t *testing.T) {
	e := goEditor("foo")
	feed(e, "A", "ctrl+/")
	if got := text(e); got != "// foo" {
		t.Fatalf("%q", got)
	}
	typeKeys(e, "!")
	if got := text(e); got != "// foo!" {
		t.Fatalf("cursor kept its place in the text: %q", got)
	}
	feed(e, "ctrl+/")
	if got := text(e); got != "foo!" {
		t.Fatalf("%q", got)
	}
	if e.Mode() != ModeInsert {
		t.Fatal("still inserting")
	}
}

func TestCtrlSlashVisual(t *testing.T) {
	e := goEditor("a\nb")
	feed(e, "V", "j", "ctrl+/")
	if got := text(e); got != "// a\n// b" || e.Mode() != ModeNormal {
		t.Fatalf("%q %v", got, e.Mode())
	}
}

func TestCommentWithoutSyntaxSaysSo(t *testing.T) {
	e := NewEditor("a")
	e.SetPath("data.json")
	feed(e, "gc", "c")
	if text(e) != "a" || e.Message() != "no comment syntax for this file type" {
		t.Fatalf("%q %q", text(e), e.Message())
	}
}

func TestCommentBlockLanguageEditor(t *testing.T) {
	e := NewEditor("a {\n  b\n}")
	e.SetPath("s.css")
	feed(e, "gc", "j")
	if got := text(e); got != "/* a { */\n/*   b */\n}" {
		t.Fatalf("%q", got)
	}
}
