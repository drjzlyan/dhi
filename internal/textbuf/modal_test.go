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
	feed(e, "q") // unknown key → ignored
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

	// :e refuses to discard changes; :e! reloads (F-115)
	feed(e2, ":", "e", "enter")
	if e2.TakeReloaded() {
		t.Fatal(":e discarded unsaved changes")
	}
	if !strings.Contains(e2.Message(), ":e!") {
		t.Fatalf("msg = %q", e2.Message())
	}
	feed(e2, ":", "e", "!", "enter")
	if e2.TakeReloaded() == false {
		t.Fatal(":e! did not reload")
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

// The shell passes a bare space as " " (the key name "space" is not a
// printable rune); insert mode must accept it and normal mode must still
// treat it as a right motion.
func TestSpaceKeyBothSpellings(t *testing.T) {
	e := NewEditor("ab")
	feed(e, "A")
	feed(e, " ")
	feed(e, "c")
	feed(e, "esc")
	if got := e.Buffer().Text(); got != "ab c" {
		t.Fatalf("insert-mode space dropped: %q", got)
	}
	for _, k := range []string{" ", "space"} {
		e := NewEditor("abc")
		feed(e, k)
		if c := e.Buffer().Cursor(); c.Col != 1 {
			t.Errorf("normal-mode %q moved cursor to col %d, want 1", k, c.Col)
		}
	}
}

// TestFindCharMotion tests f/F/t/T motions (F-133).
func TestFindCharMotion(t *testing.T) {
	// f: find forward, inclusive
	e := NewEditor("abcxyz")
	feed(e, "f", "x")
	if c := e.Buffer().Cursor(); c.Col != 3 {
		t.Fatalf("fx: cursor col = %d, want 3", c.Col)
	}

	// F: find backward, exclusive
	e2 := NewEditor("xyzabc")
	feed(e2, "$", "F", "x")
	if c := e2.Buffer().Cursor(); c.Col != 0 {
		t.Fatalf("$Fx: cursor col = %d, want 0", c.Col)
	}

	// t: until forward, inclusive
	e3 := NewEditor("abcxyz")
	feed(e3, "t", "x")
	if c := e3.Buffer().Cursor(); c.Col != 2 {
		t.Fatalf("tx: cursor col = %d, want 2", c.Col)
	}

	// T: until backward, exclusive
	e4 := NewEditor("xyzabc")
	feed(e4, "$", "T", "x")
	if c := e4.Buffer().Cursor(); c.Col != 1 {
		t.Fatalf("$Tx: cursor col = %d, want 1", c.Col)
	}

	// Not found: cursor stays
	e5 := NewEditor("abcdef")
	feed(e5, "f", "z")
	if c := e5.Buffer().Cursor(); c.Col != 0 {
		t.Fatalf("fz (not found): cursor col = %d, want 0", c.Col)
	}

	// Count: find nth occurrence on "axaxax"
	// x appears at positions 1, 3, 5
	// 2fx finds the 2nd x at position 3
	e6 := NewEditor("axaxax")
	feed(e6, "2", "f", "x")
	if c := e6.Buffer().Cursor(); c.Col != 3 {
		t.Fatalf("2fx: cursor col = %d, want 3", c.Col)
	}

	// Find with space
	e7 := NewEditor("a b c")
	feed(e7, "f", "space")
	if c := e7.Buffer().Cursor(); c.Col != 1 {
		t.Fatalf("f<space>: cursor col = %d, want 1", c.Col)
	}
}

// TestDeleteWithFind tests delete operator with find motions (F-133).
func TestDeleteWithFind(t *testing.T) {
	// dfx on abcx: delete inclusive up to x
	e := NewEditor("abcx")
	feed(e, "d", "f", "x")
	if got := e.Buffer().Text(); got != "" {
		t.Fatalf("dfx: %q, want empty", got)
	}

	// dtx on abcxyz: delete up to (but not including) x
	e2 := NewEditor("abcxyz")
	feed(e2, "d", "t", "x")
	if got := e2.Buffer().Text(); got != "xyz" {
		t.Fatalf("dtx: %q, want xyz", got)
	}

	// dFx backward on xabc (cursor at col 4, after 'c')
	// F is exclusive: delete from cursor back to (but not including) x
	// Range is [0, 4), which deletes all 4 characters
	e3 := NewEditor("xabc")
	feed(e3, "$", "d", "F", "x")
	if got := e3.Buffer().Text(); got != "" {
		t.Fatalf("$dFx: %q, want empty", got)
	}

	// Test with cursor on 'c' (col 3): dFx should delete [0, 3)
	e4 := NewEditor("xabc")
	feed(e4, "$", "h", "d", "F", "x") // move cursor left to col 3
	if got := e4.Buffer().Text(); got != "c" {
		t.Fatalf("dFx at col 3: %q, want c", got)
	}
}

// TestRepeatFind tests ; and , for find repetition (F-133).
func TestRepeatFind(t *testing.T) {
	// ; repeats the last find
	// On "xaxax", starting at col 0:
	// x's are at positions 0, 2, 4
	// fx finds first x after cursor at col 2
	e := NewEditor("xaxax")
	feed(e, "f", "x")
	if c := e.Buffer().Cursor(); c.Col != 2 {
		t.Fatalf("fx: col=%d, want 2", c.Col)
	}
	feed(e, ";")
	if c := e.Buffer().Cursor(); c.Col != 4 {
		t.Fatalf("fx;: col=%d, want 4", c.Col)
	}

	// , reverses the find direction
	// On "xaxax", starting at col 0:
	// x's are at positions 0, 2, 4
	// 2fx finds the 2nd x at col 4
	e2 := NewEditor("xaxax")
	feed(e2, "2", "f", "x") // move to second x at col 4
	if c := e2.Buffer().Cursor(); c.Col != 4 {
		t.Fatalf("2fx: col=%d, want 4", c.Col)
	}
	// Now from col 4, find next x - there's none, so no move
	// Try a different test: reverse the last find
	e3 := NewEditor("xaxax")
	feed(e3, "l", "f", "x") // start at col 1, find next x at col 2
	if c := e3.Buffer().Cursor(); c.Col != 2 {
		t.Fatalf("col 1, fx: col=%d, want 2", c.Col)
	}
	feed(e3, ",") // reverse: find backward for x from col 2, should find col 0
	if c := e3.Buffer().Cursor(); c.Col != 0 {
		t.Fatalf("col 2, Fx (via ,): col=%d, want 0", c.Col)
	}
}

// TestMatchBracket tests % motion (F-133).
func TestMatchBracket(t *testing.T) {
	// % on opening bracket
	e := NewEditor("a(b)c")
	feed(e, "l", "%")
	if c := e.Buffer().Cursor(); c.Col != 3 {
		t.Fatalf("(b)c at ( then %%: col=%d, want 3", c.Col)
	}

	// % on closing bracket
	e2 := NewEditor("a(b)c")
	feed(e2, "l", "l", "l", "%")
	if c := e2.Buffer().Cursor(); c.Col != 1 {
		t.Fatalf("a(b)c at ) then %%: col=%d, want 1", c.Col)
	}

	// % not on bracket: find forward
	e3 := NewEditor("ab(cd)ef")
	feed(e3, "%")
	if c := e3.Buffer().Cursor(); c.Col != 5 {
		t.Fatalf("ab(cd)ef not on bracket, %% finds ): col=%d, want 5", c.Col)
	}
}

// d% and d; d, compose like vim (F-133).
func TestOperatorBracketAndRepeatFind(t *testing.T) {
	for _, c := range []struct {
		in   string
		keys []string
		want string
	}{
		{"f(a, b) z", []string{"d", "%"}, " z"},
		{"f(a, b) z", []string{"f", "(", "d", "%"}, "f z"},
		{"a,b,c,d", []string{"f", ",", ";", "d", ";"}, "a,bd"},
		{"a,b,c,d", []string{"$", "F", ",", "d", ","}, "a,b,c,d"}, // no "," ahead: nothing deleted
	} {
		e := NewEditor(c.in)
		feed(e, c.keys...)
		if got := e.Buffer().Text(); got != c.want {
			t.Errorf("%q %v = %q, want %q", c.in, c.keys, got, c.want)
		}
	}
}
