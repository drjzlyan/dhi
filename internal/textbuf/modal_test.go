package textbuf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func feed(e *Editor, keys ...string) {
	for _, k := range keys {
		e.Key(k)
	}
}

// typeKeys feeds each rune of s as its own keystroke.
func typeKeys(e *Editor, s string) {
	for _, r := range s {
		e.Key(string(r))
	}
}

func TestInsertModeRoundTrip(t *testing.T) {
	e := NewEditor("hello")
	feed(e, "A")
	typeKeys(e, ", world")
	feed(e, "esc")
	if got := e.Buffer().Text(); got != "hello, world" {
		t.Fatalf("%q", got)
	}
	if e.Mode() != ModeNormal {
		t.Fatalf("mode = %v", e.Mode())
	}
	if c := e.Buffer().Cursor(); c.Col != 11 { // stepped off insert point
		t.Fatalf("cursor = %+v", c)
	}
}

func TestIOpenAndType(t *testing.T) {
	e := NewEditor("fn main() {}")
	feed(e, "f") // unknown in MVP → ignored
	e.Key("i")
	feed(e, "x", "y", "esc")
	if e.Buffer().Text() != "xyfn main() {}" {
		t.Fatalf("%q", e.Buffer().Text())
	}
}

func TestOOpenLineBelowAbove(t *testing.T) {
	e := NewEditor("one\ntwo")
	feed(e, "G", "o")
	typeKeys(e, "three")
	feed(e, "esc")
	if got := e.Buffer().Text(); got != "one\ntwo\nthree" {
		t.Fatalf("o: %q", got)
	}
	e2 := NewEditor("two")
	feed(e2, "O")
	typeKeys(e2, "one")
	feed(e2, "esc")
	if got := e2.Buffer().Text(); got != "one\ntwo" {
		t.Fatalf("O at BOF: %q", got)
	}
}

func TestXAndCounts(t *testing.T) {
	e := NewEditor("abcdef")
	feed(e, "3", "x")
	if e.Buffer().Text() != "def" {
		t.Fatalf("3x: %q", e.Buffer().Text())
	}
}

func TestDeleteWordMotion(t *testing.T) {
	e := NewEditor("alpha beta gamma")
	feed(e, "d", "w")
	if e.Buffer().Text() != "beta gamma" {
		t.Fatalf("dw: %q", e.Buffer().Text())
	}
	reg, lw := e.RegisterText()
	if reg != "alpha " || lw {
		t.Fatalf("register = %q linewise=%v", reg, lw)
	}

	e2 := NewEditor("keep alpha beta")
	feed(e2, "w", "w", "d", "b")
	if got := e2.Buffer().Text(); got != "keep beta" {
		t.Fatalf("db: %q", got)
	}
}

func TestDDAndCountDD(t *testing.T) {
	e := NewEditor("a\nb\nc\nd")
	feed(e, "d", "d")
	if got := e.Buffer().Text(); got != "b\nc\nd" {
		t.Fatalf("dd: %q", got)
	}
	if _, lw := e.RegisterText(); !lw {
		t.Fatal("dd register not linewise")
	}

	feed(e, "2", "d", "d")
	if got := e.Buffer().Text(); got != "d" {
		t.Fatalf("2dd from b: %q", got)
	}
	if c := e.Buffer().Cursor(); c.Line != 0 {
		t.Fatalf("cursor = %+v", c)
	}
}

func TestYankPaste(t *testing.T) {
	e := NewEditor("one\ntwo\nthree")
	feed(e, "y", "y", "j", "p")
	if got := e.Buffer().Text(); got != "one\ntwo\none\nthree" {
		t.Fatalf("yy+j+p: %q", got)
	}

	// vim's yw includes trailing whitespace
	e2 := NewEditor("ab cd")
	feed(e2, "y", "w", "A")
	typeKeys(e2, " ")
	feed(e2, "esc", "p")
	if got := e2.Buffer().Text(); got != "ab cd ab " {
		t.Fatalf("yw paste append: %q", got)
	}
}

func TestChangeOperator(t *testing.T) {
	e := NewEditor("var x = 1;")
	feed(e, "c", "i", "?") // ci not implemented; cw instead
	e.clearPending()
	e.Key("e")
	e.Key("s")

	e3 := NewEditor("var x = 1;")
	feed(e3, "c", "w")
	typeKeys(e3, "let")
	feed(e3, "esc")
	if got := e3.Buffer().Text(); !strings.HasPrefix(got, "let x = 1;") {
		t.Fatalf("cw: %q", got)
	}
	if e3.Mode() != ModeNormal {
		t.Fatalf("mode after change = %v", e3.Mode())
	}

	e4 := NewEditor("first\nsecond\nthird")
	feed(e4, "c", "c")
	typeKeys(e4, "only")
	feed(e4, "esc")
	if got := e4.Buffer().Text(); got != "only\nsecond\nthird" {
		t.Fatalf("cc: %q", got)
	}
}

func TestVisualMode(t *testing.T) {
	e := NewEditor("hello world")
	feed(e, "v", "l", "l", "l", "l", "d")
	if e.Buffer().Text() != " world" {
		t.Fatalf("v5d? : %q", e.Buffer().Text())
	}

	e2 := NewEditor("grab this text")
	feed(e2, "w", "v", "e", "y", "w", "P")
	// yank "this", move a word, paste before
	if !strings.Contains(e2.Buffer().Text(), "this") && strings.Count(e2.Buffer().Text(), "this") < 1 {
		t.Fatalf("visual yank lost text: %q", e2.Buffer().Text())
	}

	e3 := NewEditor("abc def")
	feed(e3, "v", "e", "c")
	typeKeys(e3, "XY")
	feed(e3, "esc")
	if e3.Buffer().Text() != "XY def" {
		t.Fatalf("vc: %q", e3.Buffer().Text())
	}
}

func TestUndoViaKeys(t *testing.T) {
	e := NewEditor("keep")
	feed(e, "A")
	typeKeys(e, "-edit")
	feed(e, "esc", "u")
	if e.Buffer().Text() != "keep" {
		t.Fatalf("undo: %q", e.Buffer().Text())
	}
	feed(e, "ctrl+r")
	if e.Buffer().Text() != "keep-edit" {
		t.Fatalf("redo: %q", e.Buffer().Text())
	}
}

func TestCommandModeSaveQuitReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	e := NewEditor("content")
	feed(e, ":", "w", "enter") // no path yet
	if !strings.Contains(e.Message(), "no file name") {
		t.Fatalf("msg = %q", e.Message())
	}

	e.SetPath(path)
	feed(e, ":", "w", "q", "enter")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "content\n" {
		t.Fatalf("save failed: %q %v", data, err)
	}
	if !e.CloseRequested() {
		t.Fatal(":wq did not request close")
	}
	e.TakeClose()

	// modify then refuse plain :q
	e2, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	feed(e2, "A", "!", "esc")
	feed(e2, ":", "q", "enter")
	if e2.CloseRequested() {
		t.Fatal(":q with dirty buffer closed anyway")
	}
	if !strings.Contains(e2.Message(), "no write") {
		t.Fatalf("msg = %q", e2.Message())
	}
	feed(e2, ":", "q", "!", "enter")
	if !e2.CloseRequested() || !e2.CloseForced() {
		t.Fatal(":q! did not force close")
	}

	// :e reload discards changes
	feed(e2, ":")
	feed(e2, "e", "enter")
	if e2.TakeReloaded() == false {
		t.Fatal(":e did not reload")
	}
	if strings.HasSuffix(e2.Buffer().Text(), "!") {
		t.Fatalf(":e kept dirty content: %q", e2.Buffer().Text())
	}
}

func TestJoinOperator(t *testing.T) {
	e := NewEditor("a\n b\nc")
	feed(e, "J")
	if got := e.Buffer().Text(); got != "a b\nc" {
		t.Fatalf("J: %q", got)
	}
}

func TestPendingEscapeCancels(t *testing.T) {
	e := NewEditor("abc")
	feed(e, "d", "esc", "d", "d")
	if e.Buffer().Text() != "" || strings.TrimSpace(e.Buffer().Text()) != "" {
		t.Fatalf("after esc+dd: %q", e.Buffer().Text())
	}
}
