package textbuf

import (
	"strings"
	"testing"
)

func TestExSubstitute(t *testing.T) {
	e := NewEditor("cat sat\ncat\n")
	// :s on the cursor line (first hit only).
	e.Key(":")
	for _, k := range []string{"s", "/", "c", "a", "t", "/", "d", "o", "g"} {
		e.Key(k)
	}
	e.Key("enter")
	if got := e.Buffer().Line(0); got != "dog sat" {
		t.Fatalf("cursor :s = %q", got)
	}
	if got := e.Buffer().Line(1); got != "cat" {
		t.Fatalf(":s leaked across lines: %q", got)
	}
	// Whole-buffer form; without g the second cat on a line survives.
	e.Buffer().SetCursor(Pos{Line: 0, Col: 0})
	e.Key(":")
	for _, k := range []string{"%", "s", "/", "c", "a", "t", "/", "d", "o", "g", "/", "g"} {
		e.Key(k)
	}
	e.Key("enter")
	if got := e.Buffer().Text(); strings.Contains(got, "cat") {
		t.Fatalf("%%s left a cat: %q", got)
	}
	// Not-a-substitute falls through to the unknown-command path.
	e.Key(":")
	for _, k := range []string{"z", "z", "z"} {
		e.Key(k)
	}
	e.Key("enter")
	if !strings.Contains(e.Message(), "not an editor command") {
		t.Fatalf("unknown ex message = %q", e.Message())
	}
}

// ex runs one ":" command through the key path.
func ex(e *Editor, cmd string) {
	e.Key(":")
	for _, r := range cmd {
		e.Key(string(r))
	}
	e.Key("enter")
}

func TestRegexSubstituteForms(t *testing.T) {
	cases := []struct{ name, text, cmd, want string }{
		{"literal first", "cat cat\n", `s/cat/dog/`, "dog cat\n"},
		{"global", "cat cat\n", `s/cat/dog/g`, "dog dog\n"},
		{"plus class", "a1b22c\n", `s/[0-9]\+/#/g`, "a#b#c\n"},
		{"groups swap", "foo bar\n", `s/\(\w\+\) \(\w\+\)/\2 \1/`, "bar foo\n"},
		{"ampersand", "ab\n", `s/b/[&]/`, "a[b]\n"},
		{"escaped ampersand", "ab\n", `s/b/\&/`, "a&\n"},
		{"word edges", "cat concat cat\n", `s/\<cat\>/X/g`, "X concat X\n"},
		{"alternation", "red blue green\n", `s/red\|green/C/g`, "C blue C\n"},
		{"optional", "color colour\n", `s/colou\?r/C/g`, "C C\n"},
		{"literal parens", "f(x)\n", `s/(x)/[y]/`, "f[y]\n"},
		{"literal plus", "a+b\n", `s/a+b/ok/`, "ok\n"},
		{"ignore case", "Cat CAT\n", `s/cat/d/gi`, "d d\n"},
		{"case sensitive default", "Cat cat\n", `s/cat/d/g`, "Cat d\n"},
		{"newline in rep", "a,b\n", `s/,/\n/`, "a\nb\n"},
		{"anchors", "abc\n", `s/$/!/`, "abc!\n"},
		{"alt separator", "a/b\n", `s#/#-#`, "a-b\n"},
		{"escaped separator", "a/b\n", `s/\//-/`, "a-b\n"},
		{"posix class", "x1y2\n", `s/[[:digit:]]/_/g`, "x_y_\n"},
		{"whole buffer", "a\na\n", `%s/a/b/`, "b\nb\n"},
	}
	for _, c := range cases {
		e := NewEditor(c.text)
		ex(e, c.cmd)
		if got := e.Buffer().Text(); got != c.want {
			t.Errorf("%s: %q -> %q, want %q (msg %q)", c.name, c.cmd, got, c.want, e.Message())
		}
	}
}

func TestRegexSubstituteRanges(t *testing.T) {
	e := NewEditor("a\na\na\na\n")
	ex(e, `2,3s/a/b/`)
	if got := e.Buffer().Text(); got != "a\nb\nb\na\n" {
		t.Fatalf("n,m = %q", got)
	}
	ex(e, `4s/a/c/`)
	if got := e.Buffer().Text(); got != "a\nb\nb\nc\n" {
		t.Fatalf("n = %q", got)
	}
	ex(e, `1,$s/b/B/`)
	if got := e.Buffer().Text(); got != "a\nB\nB\nc\n" {
		t.Fatalf("1,$ = %q", got)
	}
	// '<,'> from a visual selection.
	e = NewEditor("a\na\na\na\n")
	e.Key("j")
	e.Key("V")
	e.Key("j")
	ex2 := func(cmd string) {
		e.Key(":")
		for _, r := range cmd {
			e.Key(string(r))
		}
		e.Key("enter")
	}
	ex2(`'<,'>s/a/z/`)
	if got := e.Buffer().Text(); got != "a\nz\nz\na\n" {
		t.Fatalf("'<,'> = %q (msg %q)", got, e.Message())
	}
	ex(e, `'<,'>s/z/y/`) // marks persist after the selection ends
	if got := e.Buffer().Text(); got != "a\ny\ny\na\n" {
		t.Fatalf("marks after visual = %q", got)
	}
}

func TestRegexSubstituteErrorsLeaveBufferAlone(t *testing.T) {
	for cmd, want := range map[string]string{
		`s/\(a/x/`:     "invalid pattern",
		`s/a**/x/`:     "invalid pattern",
		`s/a/x/c`:      "c (confirm) flag is not supported",
		`s/a/x/gz`:     "unknown :s flag: z",
		`s/\(a\)\1/x/`: "backreference",
		`s/\va/x/`:     "not supported",
		`s/a/\1/`:      "replacement uses",
		`9s/a/x/`:      "invalid range",
		`3,2s/a/x/`:    "backwards range",
		`'<,'>s/a/x/`:  "no visual selection",
		`s/a`:          "usage",
		`s//x/`:        "empty pattern",
		`%s/zzz/x/`:    "pattern not found",
	} {
		e := NewEditor("a\nb\nc\n")
		ex(e, cmd)
		if !strings.Contains(e.Message(), want) {
			t.Errorf("%s: message %q, want %q", cmd, e.Message(), want)
		}
		if e.Buffer().Text() != "a\nb\nc\n" || e.Buffer().Dirty() {
			t.Errorf("%s: buffer changed: %q", cmd, e.Buffer().Text())
		}
	}
}

func TestRegexSubstituteOneUndoStep(t *testing.T) {
	e := NewEditor("a1\na2\na3\n")
	ex(e, `%s/a\(\d\)/\1/`)
	if got := e.Buffer().Text(); got != "1\n2\n3\n" {
		t.Fatalf("sub = %q", got)
	}
	e.Key("u")
	if got := e.Buffer().Text(); got != "a1\na2\na3\n" {
		t.Fatalf("one undo should restore all: %q", got)
	}
}

func TestTranslateVimPattern(t *testing.T) {
	for in, want := range map[string]string{
		`\<a\>`:     `\ba\b`,
		`\(x\)\+`:   `(x)+`,
		`a\|b`:      `a|b`,
		`(a|b)+?{}`: `\(a\|b\)\+\?\{\}`,
		`[a(b]`:     `[a(b]`,
		`a\.b`:      `a\.b`,
		`\=`:        `?`,
	} {
		got, err := translateVimPattern(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q (%v), want %q", in, got, err, want)
		}
	}
}
