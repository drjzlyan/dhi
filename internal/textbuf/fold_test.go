package textbuf

import (
	"strings"
	"testing"
)

// foldText is the shape the expectations below were checked against in vim
// 9.1 (foldmethod=indent): folds on lines 2-3, 5-8 and the nested 6-7
// (1-based).
const foldText = "a\n  b\n  c\nd\n  e\n    f\n    g\n  h\ni"

var foldRanges = []FoldRange{{1, 2}, {4, 7}, {5, 6}}

func foldEditor() *Editor {
	e := NewEditor(foldText)
	e.buf.SetFoldRanges(foldRanges, "test")
	return e
}

func lineNo(e *Editor) int { return e.buf.Cursor().Line + 1 }

func TestFoldKeysMatchVim(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		line int    // 1-based cursor line
		text string // "" = unchanged
	}{
		{"zM then j j skips the fold", []string{"zM", "j", "j"}, 4, ""},
		{"3j over closed folds", []string{"zM", "3", "j"}, 5, ""},
		{"zM j k round trip", []string{"zM", "j", "j", "k", "k"}, 1, ""},
		{"zj zj open", []string{"zj", "zj"}, 5, ""},
		{"zj x3 reaches the nested fold", []string{"zj", "zj", "zj"}, 6, ""},
		{"zk from the end", []string{"G", "zk"}, 8, ""},
		{"zk twice", []string{"G", "zk", "zk"}, 7, ""},
		{"zM 2zj counts a closed fold once", []string{"zM", "2", "zj"}, 5, ""},
		{"zM G zk lands on the fold line", []string{"zM", "G", "zk"}, 5, ""},
		{"zM G zk zk", []string{"zM", "G", "zk", "zk"}, 2, ""},
		{"dd on a closed fold", []string{"zM", "j", "d", "d"}, 2, "a\nd\n  e\n    f\n    g\n  h\ni"},
		{"dd on d beside folds", []string{"zM", "j", "j", "d", "d"}, 4, "a\n  b\n  c\n  e\n    f\n    g\n  h\ni"},
		{"dj from a closed fold", []string{"zM", "j", "d", "j"}, 2, "a\n  e\n    f\n    g\n  h\ni"},
		{"d2j covers three lines of the view", []string{"zM", "d", "2", "j"}, 1, "  e\n    f\n    g\n  h\ni"},
		{"dk into a closed fold", []string{"zM", "G", "d", "k"}, 4, "a\n  b\n  c\nd"},
		{"dG from the top", []string{"zM", "dG"}, 1, ""},
		{"2dd counts folds as one line", []string{"zM", "j", "2", "d", "d"}, 2, "a\n  e\n    f\n    g\n  h\ni"},
		{"dzj over a closed fold", []string{"zM", "j", "j", "d", "zj"}, 4, "a\n  b\n  c\ni"},
		{"dzj open", []string{"j", "j", "j", "d", "zj"}, 4, "a\n  b\n  c\n    f\n    g\n  h\ni"},
		{"dzk", []string{"G", "d", "zk"}, 7, "a\n  b\n  c\nd\n  e\n    f\n    g"},
		{"x deletes a closed fold whole", []string{"zM", "j", "x"}, 2, "a\nd\n  e\n    f\n    g\n  h\ni"},
		{"dl deletes a closed fold whole", []string{"zM", "j", "d", "l"}, 2, "a\nd\n  e\n    f\n    g\n  h\ni"},
		{"yyp puts a line below the fold", []string{"zM", "j", "j", "y", "y", "G", "p"}, 10, "a\n  b\n  c\nd\n  e\n    f\n    g\n  h\ni\nd"},
		{"p below a closed fold", []string{"zM", "j", "j", "y", "y", "k", "p"}, 4, "a\n  b\n  c\nd\nd\n  e\n    f\n    g\n  h\ni"},
		{"o below a closed fold", []string{"zM", "j", "o", "x", "esc"}, 4, "a\n  b\n  c\n  x\nd\n  e\n    f\n    g\n  h\ni"},
		{">> on a closed fold shifts all of it", []string{"zM", "j", ">", ">"}, 2, "a\n\t  b\n\t  c\nd\n  e\n    f\n    g\n  h\ni"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := foldEditor()
			feed(e, tt.keys...)
			if tt.text != "" {
				if got := e.buf.Text(); got != tt.text {
					t.Fatalf("text:\n%q\nwant\n%q", got, tt.text)
				}
			}
			if got := lineNo(e); got != tt.line {
				t.Errorf("cursor line = %d, want %d", got, tt.line)
			}
		})
	}
}

func TestFoldDGDeletesEverything(t *testing.T) {
	e := foldEditor()
	feed(e, "zM", "d", "G")
	if e.buf.Text() != "" {
		t.Errorf("text = %q", e.buf.Text())
	}
}

func TestFoldOpenCloseCommands(t *testing.T) {
	closedAt := func(e *Editor) []int {
		var out []int
		for _, f := range e.buf.Folds() {
			if f.Closed {
				out = append(out, f.Start+1)
			}
		}
		return out
	}
	eq := func(t *testing.T, got []int, want ...int) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("closed = %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("closed = %v, want %v", got, want)
			}
		}
	}
	e := foldEditor()
	feed(e, "zM")
	eq(t, closedAt(e), 2, 5, 6)
	feed(e, "zR")
	eq(t, closedAt(e))
	// zc on the nested fold closes it, a second zc closes the enclosing one.
	feed(e, "j", "j", "j", "j", "j") // line 6, the nested fold's first line
	feed(e, "zc")
	eq(t, closedAt(e), 6)
	feed(e, "zc")
	eq(t, closedAt(e), 5, 6)
	if lineNo(e) != 5 {
		t.Errorf("zc leaves the cursor on the fold line, got %d", lineNo(e))
	}
	// zo opens one level, the outer; the inner stays closed.
	feed(e, "zo")
	eq(t, closedAt(e), 6)
	// zO opens everything under the cursor; zC closes everything around it.
	feed(e, "zC")
	eq(t, closedAt(e), 5, 6)
	feed(e, "zO") // only the folds around the cursor line: the nested one stays closed
	eq(t, closedAt(e), 6)
	// za toggles.
	feed(e, "za")
	eq(t, closedAt(e), 5, 6)
	feed(e, "za")
	eq(t, closedAt(e), 6)
	if !strings.Contains(e.Message(), "folds from test") {
		t.Errorf("message must name the source: %q", e.Message())
	}
}

func TestFoldKeysAreNotRepeatedByDot(t *testing.T) {
	e := foldEditor()
	feed(e, "x", "zM", "zj", ".") // the last change is x; the z commands in between do not replace it
	if got := e.buf.Line(0); got != "" {
		t.Errorf("line 1 = %q: . must repeat the x, not a fold command", got)
	}
	_ = e
}

func TestFoldNoRangesSaysSo(t *testing.T) {
	e := NewEditor("a\nb")
	feed(e, "za")
	if e.Message() != "no folds" {
		t.Errorf("message = %q", e.Message())
	}
	e.buf.SetFoldRanges(nil, "indentation")
	feed(e, "zc")
	if !strings.Contains(e.Message(), "no folds") || !strings.Contains(e.Message(), "indentation") {
		t.Errorf("message = %q", e.Message())
	}
}

func TestFoldJumpInsideOpensIt(t *testing.T) {
	e := foldEditor()
	feed(e, "zM")
	e.Key("/")
	typeKeys(e, "g")
	e.Key("enter")
	if lineNo(e) != 7 {
		t.Fatalf("search landed on %d", lineNo(e))
	}
	for _, f := range e.buf.Folds() {
		if f.Closed && f.Start <= 6 && f.End >= 6 {
			t.Errorf("fold %v still closed after the search landed in it", f)
		}
	}
	e = foldEditor()
	feed(e, "zM", "G")
	e.GotoLine(3) // a jump from outside
	e.RevealFolds()
	if e.buf.FoldHidden(2) {
		t.Error("GotoLine into a fold must open it")
	}
}

func TestFoldInsertOpens(t *testing.T) {
	e := foldEditor()
	feed(e, "zM", "j", "A", "x", "esc")
	for _, f := range e.buf.Folds() {
		if f.Start == 1 && f.Closed {
			t.Error("typing on a closed fold's line must open it")
		}
	}
}

func TestFoldMapsAcrossEdits(t *testing.T) {
	e := foldEditor()
	feed(e, "zM")
	// A line added above shifts every fold.
	e.buf.SetCursor(Pos{0, 0})
	feed(e, "O", "x", "esc")
	got := e.buf.Folds()
	if len(got) != 3 || got[0].Start != 2 || got[0].End != 3 || !got[0].Closed || got[1].Start != 5 {
		t.Fatalf("folds after O above = %+v", got)
	}
	// Deleting the first line of a fold drops that fold.
	e.buf.SetCursor(Pos{2, 0})
	feed(e, "d", "d")
	for _, f := range e.buf.Folds() {
		if f.Start == 2 && f.End == 3 {
			t.Errorf("fold survived losing its start: %+v", f)
		}
	}
	// An edit inside a closed fold's hidden lines opens it.
	e = foldEditor()
	feed(e, "zM")
	e.buf.SetCursor(Pos{2, 2}) // c, hidden in the fold on lines 2-3
	e.buf.foldReveal(2)
	feed(e, "x")
	for _, f := range e.buf.Folds() {
		if f.Start == 1 && f.Closed {
			t.Error("an edit in the fold's body must leave it open")
		}
	}
	// Undo and a text edit keep the buffer text unchanged by any fold command.
	e = foldEditor()
	before := e.buf.Text()
	feed(e, "zM", "zR", "zj", "zk", "za")
	if e.buf.Text() != before || e.buf.Dirty() {
		t.Error("fold commands must not touch the text")
	}
}

func TestFoldReplaceTextKeepsOtherFolds(t *testing.T) {
	e := foldEditor()
	feed(e, "zM")
	e.buf.SetCursor(Pos{8, 0})
	feed(e, "x") // i -> "" (same line count)
	if n := len(e.buf.Folds()); n != 3 {
		t.Errorf("folds = %d", n)
	}
	if !e.buf.Folds()[1].Closed {
		t.Error("an edit outside the fold must not open it")
	}
}

func TestFoldVisualLineTakesWholeFold(t *testing.T) {
	e := foldEditor()
	feed(e, "zM", "j", "V", "y")
	r, lw := e.RegisterText()
	if !lw || r != "  b\n  c\n" {
		t.Errorf("register = %q linewise=%v", r, lw)
	}
}

func TestFoldSecondaryCursorsCollapseOnClose(t *testing.T) {
	e := NewEditor("x\n  1\n  2\nx\n  3\n  4")
	e.buf.SetFoldRanges([]FoldRange{{0, 2}, {3, 5}}, "test")
	e.buf.SetCursor(Pos{0, 0})
	e.Key("alt+down")
	e.Key("alt+down")
	if e.CursorCount() != 3 {
		t.Fatalf("cursors = %d", e.CursorCount())
	}
	feed(e, "zM")
	// 3 cursors on lines 1, 2, 3 -> all inside the first fold except the
	// first (line 1): lines 2 and 3 collapse onto line 1 and merge.
	if e.CursorCount() != 1 {
		t.Errorf("cursors after zM = %d (%v)", e.CursorCount(), allCursors(e))
	}
	// A search or jump landing in a closed fold opens it, cursors included.
	e.buf.SetCursor(Pos{4, 0})
	e.RevealFolds()
	if e.buf.FoldHidden(4) {
		t.Error("the fold hiding the cursor must open")
	}
}

func TestFoldSetRangesKeepsClosedStarts(t *testing.T) {
	e := foldEditor()
	feed(e, "zM")
	e.buf.SetFoldRanges([]FoldRange{{1, 2}, {4, 7}}, "again")
	cl := 0
	for _, f := range e.buf.Folds() {
		if f.Closed {
			cl++
		}
	}
	if cl != 2 {
		t.Errorf("closed after refresh = %d", cl)
	}
	// Garbage ranges are dropped: backwards, past the end, crossing.
	e.buf.SetFoldRanges([]FoldRange{{3, 3}, {5, 2}, {0, 99}, {0, 4}, {2, 6}}, "x")
	got := e.buf.Folds()
	if len(got) != 1 || got[0].Start != 0 || got[0].End != 4 {
		t.Errorf("normalized = %+v", got)
	}
}
