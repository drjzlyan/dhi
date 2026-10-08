package syntax

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

const goSrc = "package main\n\n// hello\nfunc main() {\n\tx := 42 + 1\n\tprintln(\"hi\")\n}\n"

func TestLexRoundTripsTextLineByLine(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	lines := Lex("main.go", goSrc)
	if lines == nil {
		t.Fatal("go not lexed")
	}
	src := strings.Split(goSrc, "\n")
	if len(lines) != len(src) {
		t.Fatalf("%d lines, source has %d", len(lines), len(src))
	}
	for i, l := range lines {
		if got := l.Text(); got != src[i] {
			t.Errorf("line %d = %q, want %q", i, got, src[i])
		}
		if got := ansi.Strip(Render(l)); got != src[i] {
			t.Errorf("line %d rendered %q, want %q", i, got, src[i])
		}
		if len(l.Classes()) != len([]rune(src[i])) {
			t.Errorf("line %d: %d classes for %d runes", i, len(l.Classes()), len([]rune(src[i])))
		}
	}
}

func TestClassesAreAssigned(t *testing.T) {
	lines := Lex("main.go", goSrc)
	has := func(line int, c Class) bool {
		for _, x := range lines[line].Classes() {
			if x == c {
				return true
			}
		}
		return false
	}
	for _, tc := range []struct {
		line int
		want Class
		what string
	}{{2, Comment, "comment"}, {3, Keyword, "func keyword"}, {4, Number, "number"}, {5, String, "string"}} {
		if !has(tc.line, tc.want) {
			t.Errorf("line %d has no %s: %v", tc.line, tc.what, lines[tc.line])
		}
	}
}

func TestMultiLineTokensKeepTheirClass(t *testing.T) {
	lines := Lex("a.go", "/* one\ntwo */\nvar x int\n")
	for i := 0; i < 2; i++ {
		for _, c := range lines[i].Classes() {
			if c != Comment {
				t.Fatalf("line %d of a block comment lexed as %v", i, c)
			}
		}
	}
}

func TestUnlexableInputIsNilNotFakeColoured(t *testing.T) {
	for name, path := range map[string]string{"unknown": "x.unknownext", "empty": "main.go"} {
		text := "plain text\n"
		if name == "empty" {
			text = ""
		}
		if got := Lex(path, text); got != nil {
			t.Errorf("%s: got %v", name, got)
		}
	}
	if Lex("main.go", strings.Repeat("a", MaxSize+1)) != nil {
		t.Error("an oversized input was lexed")
	}
}

func TestPlainHasNoStyleAndStylesFollowTheTheme(t *testing.T) {
	if _, ok := Plain.Style(); ok {
		t.Fatal("Plain must render unstyled")
	}
	theme.SwapForTest(t, theme.Dark())
	d, _ := Keyword.Style()
	theme.SwapForTest(t, theme.Light())
	l, _ := Keyword.Style()
	if d.Render("x") == l.Render("x") {
		t.Fatal("class colours do not come from the theme")
	}
}

// chroma's InCategory matches a whole category: numbers were classed as
// strings and every identifier as a function name until the sub-category fix.
func TestNumbersAreNotStringsAndVariablesAreNotFunctions(t *testing.T) {
	lines := Lex("main.go", goSrc)
	classOf := func(line int, word string) Class {
		col := strings.Index(strings.Split(goSrc, "\n")[line], word)
		return lines[line].Classes()[col]
	}
	if got := classOf(4, "42"); got != Number {
		t.Errorf("42 = %v, want Number", got)
	}
	if got := classOf(4, "x"); got != Plain {
		t.Errorf("variable x = %v, want Plain", got)
	}
	if got := classOf(5, "\"hi\""); got != String {
		t.Errorf("\"hi\" = %v, want String", got)
	}
	if got := classOf(3, "main"); got != Func {
		t.Errorf("func name = %v, want Func", got)
	}
	if got := classOf(4, ":="); got != Operator {
		t.Errorf(":= = %v, want Operator", got)
	}
}
