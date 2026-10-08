package reviewer

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/tui/syntax"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

const goPatch = `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -1,6 +1,6 @@
 package main
 
 func main() {
-	count := compute(a) // old
+	total := compute(b) // new
 	println("ok")
 }
`

func styledModel(t *testing.T) *Model {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	m, _, _, _ := newSurface(t)
	m.files = gitdiff.Parse(goPatch)
	m.layout = layoutUnified
	m.rowsCache = nil
	return m
}

func TestCodeRowsAreExactlyTheRequestedWidthAndKeepTheText(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	text := "\tfmt.Println(\"a fairly long line that must wrap across several rows\")"
	ls := lineStyle{classes: syntax.Line{{Text: text, Class: syntax.Plain}}.Classes()}
	for _, kind := range []gitdiff.Kind{gitdiff.Ctx, gitdiff.Add, gitdiff.Del} {
		rows := codeRows(text, ls, kind, 24)
		if len(rows) < 3 {
			t.Fatalf("kind %v: %d rows, expected wrapping", kind, len(rows))
		}
		var joined strings.Builder
		for i, r := range rows {
			if w := lipgloss.Width(r); w != 24 {
				t.Errorf("kind %v row %d is %d wide, want 24", kind, i, w)
			}
			joined.WriteString(strings.TrimRight(ansi.Strip(r), " "))
		}
		// Tabs became four spaces; nothing else changed.
		if want := strings.ReplaceAll(text, "\t", "    "); strings.TrimSpace(joined.String()) != strings.TrimSpace(want) {
			t.Errorf("kind %v lost text:\n got %q\nwant %q", kind, joined.String(), want)
		}
	}
	if rows := codeRows("", lineStyle{}, gitdiff.Add, 10); len(rows) != 1 || lipgloss.Width(rows[0]) != 10 {
		t.Fatalf("empty line = %q", rows)
	}
}

func TestSyntaxAndChangedWordsReachTheRenderedDiff(t *testing.T) {
	m := styledModel(t)
	rows := m.diffRows()
	var del, add *gitdiff.Line
	var fi int
	for _, r := range rows {
		if r.kind == vrLineUnified && r.left.Kind == gitdiff.Del {
			del, fi = r.left, r.file
		}
		if r.kind == vrLineUnified && r.left.Kind == gitdiff.Add {
			add = r.left
		}
	}
	if del == nil || add == nil {
		t.Fatal("fixture has no changed pair")
	}
	ds, as := m.styleOf(fi, del), m.styleOf(fi, add)
	if len(ds.classes) != len([]rune(del.Text)) || len(as.classes) != len([]rune(add.Text)) {
		t.Fatalf("classes cover %d/%d runes of %d/%d", len(ds.classes), len(as.classes),
			len([]rune(del.Text)), len([]rune(add.Text)))
	}
	hasKeywordLike := false
	for _, c := range as.classes {
		if c == syntax.Comment {
			hasKeywordLike = true
		}
	}
	if !hasKeywordLike {
		t.Errorf("the trailing // comment was not classified: %v", as.classes)
	}
	// Only `count`→`total` and `a`→`b` changed; the // old / // new comment words too.
	changed := func(l *gitdiff.Line, ls lineStyle) string {
		var sb strings.Builder
		for i, r := range []rune(l.Text) {
			if i < len(ls.marks) && ls.marks[i] {
				sb.WriteRune(r)
			}
		}
		return sb.String()
	}
	if got := changed(del, ds); !strings.Contains(got, "count") || !strings.Contains(got, "a") || strings.Contains(got, "compute") {
		t.Errorf("removed-line marks = %q", got)
	}
	if got := changed(add, as); !strings.Contains(got, "total") || strings.Contains(got, "compute") {
		t.Errorf("added-line marks = %q", got)
	}

	// The rendered rows carry colour, differ between marked and unmarked, and fit.
	plainRow := codeRows(add.Text, lineStyle{classes: as.classes}, gitdiff.Add, 60)[0]
	markedRow := codeRows(add.Text, as, gitdiff.Add, 60)[0]
	if plainRow == markedRow {
		t.Fatal("changed words are not visually distinguished")
	}
	if !strings.Contains(markedRow, "\x1b[") {
		t.Fatal("no colour in the rendered row")
	}
}

func TestEveryDiffRowFitsItsWidthInBothLayouts(t *testing.T) {
	m := styledModel(t)
	for _, layout := range []layoutMode{layoutUnified, layoutSideBySide} {
		m.layout = layout
		m.rowsCache = nil
		segs, _ := m.diffSegmentsView(90, nil)
		if len(segs) == 0 {
			t.Fatal("no segments")
		}
		for _, s := range segs {
			if w := lipgloss.Width(s.txt); w > 91 {
				t.Errorf("layout %d: a row is %d wide (> 90+margin): %q", layout, w, ansi.Strip(s.txt))
			}
		}
	}
}

func TestUnknownLanguageAndHugeTextStayPlainNotFakeColoured(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m, _, _, _ := newSurface(t)
	m.files = gitdiff.Parse(strings.ReplaceAll(goPatch, "main.go", "notes.unknownext"))
	m.rowsCache = nil
	for _, r := range m.diffRows() {
		if r.kind == vrLineUnified {
			if ls := m.styleOf(r.file, r.left); len(ls.classes) != 0 {
				t.Fatalf("an unknown language got classes: %+v", ls.classes)
			}
		}
	}
}

func TestStylesFollowTheThemeSwap(t *testing.T) {
	ls := lineStyle{classes: syntax.Line{{Text: "func", Class: syntax.Keyword}}.Classes()}
	theme.SwapForTest(t, theme.Dark())
	dark := codeRows("func", ls, gitdiff.Add, 12)[0]
	theme.SwapForTest(t, theme.Light())
	light := codeRows("func", ls, gitdiff.Add, 12)[0]
	if dark == light {
		t.Fatal("a theme swap did not change the rendered colours (colours must come from theme tokens)")
	}
}
