package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// F-176: code folding. Lines (0-based): main 2..7 holding the if 3..5, and
// other 9..10, by indentation.
const foldBody = "package main\n\nfunc main() {\n\tif true {\n\t\ta()\n\t\tb()\n\t}\n\tc()\n}\nfunc other() {\n\td()\n}\n"

func foldOpen(t *testing.T, wrap bool) *Model {
	t.Helper()
	m := openText(t, foldBody, wrap)
	m.Resize(100, 30)
	m.View()
	return m
}

func TestIndentFoldsFallback(t *testing.T) {
	b := textbuf.New(foldBody)
	got := indentFolds(b, 4)
	want := []textbuf.FoldRange{{Start: 3, End: 5}, {Start: 2, End: 7}, {Start: 9, End: 10}}
	if len(got) != len(want) {
		t.Fatalf("folds = %v, want %v", got, want)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("missing %v in %v", w, got)
		}
	}
	// Blank lines inside belong to the fold, trailing ones do not.
	got = indentFolds(textbuf.New("a\n\tb\n\n\tc\n\n\nd"), 4)
	if len(got) != 1 || got[0] != (textbuf.FoldRange{Start: 0, End: 3}) {
		t.Errorf("blank handling: %v", got)
	}
}

func TestFoldsHideLinesAndNameTheirSource(t *testing.T) {
	m := foldOpen(t, false)
	feed(m, "z", "M")
	v := plainView(m)
	if !strings.Contains(v, "func main() { ⋯ 5 lines") || !strings.Contains(v, "func other() { ⋯ 1 line") {
		t.Fatalf("closed folds not drawn as one row:\n%s", v)
	}
	if strings.Contains(v, "a()") || strings.Contains(v, "c()") {
		t.Errorf("hidden lines are drawn:\n%s", v)
	}
	if msg := m.active().Message(); !strings.Contains(msg, "folds closed") || !strings.Contains(msg, "folds from indentation (no language server") {
		t.Errorf("status must name the indentation fallback: %q", msg)
	}
	// j skips the closed fold: package, blank, main, closing brace.
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 2})
	feed(m, "j")
	if c := m.active().Buffer().Cursor().Line; c != 8 {
		t.Errorf("j from a closed fold went to line %d, want 8", c)
	}
	feed(m, "k")
	if c := m.active().Buffer().Cursor().Line; c != 2 {
		t.Errorf("k went to line %d, want 2", c)
	}
	if m.active().Buffer().Text() != foldBody || m.active().Buffer().Dirty() {
		t.Error("folding must not touch the text")
	}
}

func TestFoldGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	m := foldOpen(t, false)
	feed(m, "j", "j", "z", "M", "j")
	golden.Snapshot(t, "editor_fold_closed_100x30", m.View())
}

func TestFoldInWrapMode(t *testing.T) {
	m := openText(t, "func a() {\n\t"+words(40)+"\n}\nlast\n", true)
	m.Resize(80, 30)
	m.View()
	feed(m, "z", "M")
	v := plainView(m)
	if !strings.Contains(v, "func a() { ⋯ 1 line") || strings.Contains(v, "wordb wordc") {
		t.Fatalf("wrapped view with a closed fold:\n%s", v)
	}
	feed(m, "j")
	if c := m.active().Buffer().Cursor().Line; c != 2 {
		t.Errorf("j over the fold = line %d", c)
	}
	feed(m, "z", "R", "j")
	if !strings.Contains(plainView(m), "wordn") {
		t.Errorf("zR must show the wrapped line again:\n%s", plainView(m))
	}
}

func rowOfText(m *Model, s string) int {
	for i, r := range strings.Split(plainView(m), "\n") {
		if strings.Contains(r, s) {
			return i
		}
	}
	return -1
}

func TestFoldClicks(t *testing.T) {
	m := foldOpen(t, false)
	feed(m, "z", "M")
	m.View()
	g := m.bufGeom
	y := rowOfText(m, "func main() {")
	if y < 0 {
		t.Fatal("no fold row")
	}
	// The row below the closed fold is line 8; a click in its text lands there.
	if p, ok := m.bufferPos(g.ox+g.textX+2, y+1); !ok || p.Line != 8 {
		t.Fatalf("click under a closed fold = %+v %v", p, ok)
	}
	if !m.Click(g.ox+g.textX-1, y) { // the ▸ marker opens it
		t.Fatal("marker click not handled")
	}
	if strings.Contains(plainView(m), "⋯") && strings.Contains(plainView(m), "func main() { ⋯") {
		t.Errorf("marker click did not open the fold:\n%s", plainView(m))
	}
	if c := m.active().Buffer().Cursor().Line; c != 2 {
		t.Errorf("cursor = %d", c)
	}
	m.View()
	if !strings.Contains(plainView(m), theme.GlyphFoldOpen) {
		t.Error("the cursor line's open fold shows its marker")
	}
	if !m.Click(g.ox+g.textX-1, y) { // and the ▾ closes it again
		t.Fatal("second marker click not handled")
	}
	if !strings.Contains(plainView(m), "func main() { ⋯ 5 lines") {
		t.Errorf("▾ click did not close:\n%s", plainView(m))
	}
}

func TestFoldSearchLandingOpens(t *testing.T) {
	m := foldOpen(t, false)
	feed(m, "z", "M")
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 0})
	feed(m, "/")
	typeKeys(m, "b()")
	feed(m, "enter")
	v := plainView(m)
	if !strings.Contains(v, "b()") || strings.Contains(v, "main() { ⋯") {
		t.Errorf("a search into a closed fold must open it:\n%s", v)
	}
}

func TestFoldMultiCursorOpensOrCollapses(t *testing.T) {
	m := foldOpen(t, false)
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 1})
	feed(m, "alt+down", "alt+down") // cursors on lines 2 and 3
	feed(m, "z", "M")
	if n := m.active().CursorCount(); n >= 3 {
		t.Errorf("closing folds must collapse cursors into the fold line, still %d", n)
	}
	m.View()
}

func TestFoldSharedBetweenPanes(t *testing.T) {
	m := foldOpen(t, false)
	feed(m, "ctrl+w", "v")
	feed(m, "z", "M")
	m.View()
	if n := strings.Count(plainView(m), "⋯ 5 lines"); n != 2 {
		t.Errorf("both panes show the buffer's folds, got %d closed rows", n)
	}
}

func foldLSPModel(t *testing.T, cfg func(*goplusFake)) (*Model, *goplusFake) {
	t.Helper()
	foldDebounce = 0
	srv, mgr := startFakeServerCfg(t, "go", false, func(f *goplusFake) {
		f.foldCap = true
		f.foldRanges = []map[string]any{{"startLine": 2, "endLine": 7}, {"startLine": 3, "endLine": 5}}
		if cfg != nil {
			cfg(f)
		}
	})
	m := newEditor(t)
	m.lspMgr = mgr
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	if err := os.WriteFile(path, []byte(foldBody), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.active() == nil {
		t.Fatal("app.go did not open")
	}
	m.prefs = func() Prefs { return Prefs{TabWidth: 4, LineNumbers: true, Scrollback: 1000} }
	return m, srv
}

func TestFoldRangesFromTheServer(t *testing.T) {
	m, srv := foldLSPModel(t, nil)
	m.View()
	if src := m.active().Buffer().FoldSource(); !strings.Contains(src, "waiting for the server") {
		t.Errorf("before the answer the status must say it waits, not use indentation: %q", src)
	}
	if n := len(m.active().Buffer().Folds()); n != 0 {
		t.Errorf("no folds before the server answers, got %d", n)
	}
	settle(t, m, func() bool { return len(m.active().Buffer().Folds()) == 2 }, "server folds")
	if src := m.active().Buffer().FoldSource(); src != "fake-gopls" {
		t.Errorf("source = %q", src)
	}
	// The server's folds are the only ones: other (9..10) is not foldable.
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 9})
	feed(m, "z", "c")
	if msg := m.active().Message(); !strings.Contains(msg, "no fold here") || !strings.Contains(msg, "folds from fake-gopls") {
		t.Errorf("message = %q", msg)
	}
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 2})
	feed(m, "z", "c")
	settle(t, m, func() bool { return strings.Contains(plainView(m), "func main() { ⋯ 5 lines") }, "closed fold")
	if srv.foldAsks == 0 {
		t.Error("no foldingRange request seen")
	}
}

func TestFoldServerFailureFallsBackNamed(t *testing.T) {
	m, _ := foldLSPModel(t, func(f *goplusFake) { f.foldFail = true })
	settle(t, m, func() bool { return strings.Contains(m.active().Buffer().FoldSource(), "folding failed") }, "fallback")
	if !strings.Contains(m.active().Buffer().FoldSource(), "indentation (fake-gopls folding failed") {
		t.Errorf("source = %q", m.active().Buffer().FoldSource())
	}
	if len(m.active().Buffer().Folds()) == 0 {
		t.Error("the named fallback must supply folds")
	}
}

func TestFoldServerWithoutCapabilityIsNamed(t *testing.T) {
	m, _ := foldLSPModel(t, func(f *goplusFake) { f.foldCap = false })
	m.View()
	if src := m.active().Buffer().FoldSource(); !strings.Contains(src, "fake-gopls offers no folding ranges") {
		t.Errorf("source = %q", src)
	}
}

func TestFoldKeepsStickyRowRight(t *testing.T) {
	// sticky scroll counts the cursor's row among the drawn rows, not lines
	m := foldOpen(t, false)
	feed(m, "z", "M")
	m.View() // must not panic with folds above the cursor and sticky on
}
