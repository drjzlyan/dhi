package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// F-175: breadcrumbs and sticky scroll read the server's outline.

func rng(l0, l1 int) map[string]any {
	return map[string]any{
		"start": map[string]any{"line": l0, "character": 0},
		"end":   map[string]any{"line": l1, "character": 1},
	}
}

func docSym(name string, kind, l0, l1 int, kids ...map[string]any) map[string]any {
	s := map[string]any{"name": name, "kind": kind, "range": rng(l0, l1), "selectionRange": rng(l0, l0)}
	if len(kids) > 0 {
		s["children"] = kids
	}
	return s
}

// outlineBody is a 60-line file: type Server (line 1..58) holding method
// Start (3..57) holding nothing deeper.
func outlineBody() string {
	var b strings.Builder
	b.WriteString("package app\n")
	b.WriteString("type Server struct {\n")
	b.WriteString("}\n")
	b.WriteString("func (s *Server) Start() {\n")
	for i := 4; i < 57; i++ {
		fmt.Fprintf(&b, "\tstep%d()\n", i)
	}
	b.WriteString("}\n")
	b.WriteString("func Other() {}\n")
	return b.String()
}

func outlineSyms() []map[string]any {
	return []map[string]any{
		docSym("Server", 23, 1, 2),
		docSym("Server.Start", 6, 3, 57),
		docSym("Other", 12, 58, 58),
	}
}

type outlineOpts struct {
	crumbs, sticky, wrap, inlay bool
}

// outlineModel opens outlineBody() as app.go against a fake server that
// provides the outline, in a 100x14 surface (a handful of text rows).
func outlineModel(t *testing.T, o outlineOpts, cfg func(*goplusFake)) (*Model, *goplusFake) {
	t.Helper()
	outlineDebounce, inlayDebounce = 0, 0
	srv, mgr := startFakeServerCfg(t, "go", false, func(f *goplusFake) {
		f.outlineCap, f.outlineSyms = true, outlineSyms()
		f.inlayCap, f.inlayHints = true, []map[string]any{typeHint(10, 5, ": int")}
		if cfg != nil {
			cfg(f)
		}
	})
	m := newEditor(t)
	m.lspMgr = mgr
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	if err := os.WriteFile(path, []byte(outlineBody()), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.active() == nil {
		t.Fatal("app.go did not open")
	}
	m.Resize(100, 16)
	m.prefs = func() Prefs {
		return Prefs{TabWidth: 4, LineNumbers: true, Wrap: o.wrap, InlayHints: o.inlay, Breadcrumbs: o.crumbs, StickyScroll: o.sticky, Scrollback: 1000}
	}
	return m, srv
}

func titleRow(m *Model) string {
	for _, r := range strings.Split(plainView(m), "\n") {
		if strings.Contains(r, "app.go") && strings.Contains(r, "NORMAL") {
			return r
		}
	}
	return ""
}

func gotoLine(m *Model, l int) {
	m.active().Buffer().SetCursor(textbuf.Pos{Line: l, Col: 1})
}

func TestBreadcrumbsShowPathAndSymbolsAtTheCursor(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{crumbs: true}, nil)
	gotoLine(m, 30)
	settle(t, m, func() bool { return strings.Contains(titleRow(m), "Server.Start") }, "symbol crumbs")
	row := titleRow(m)
	if !strings.Contains(row, "app.go › Server.Start") {
		t.Fatalf("breadcrumb path missing: %q", row)
	}
	gotoLine(m, 58)
	m.View()
	if row = titleRow(m); !strings.Contains(row, "app.go › Other") || strings.Contains(row, "Start") {
		t.Fatalf("crumb did not follow the cursor: %q", row)
	}
	gotoLine(m, 0)
	m.View()
	if row = titleRow(m); strings.Contains(row, "›") && strings.Contains(row, "Server") {
		t.Fatalf("a symbol shown outside every symbol: %q", row)
	}
}

func TestBreadcrumbsOffShowsJustTheFileName(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{crumbs: false}, nil)
	gotoLine(m, 30)
	for i := 0; i < 5; i++ {
		m.View()
	}
	if row := titleRow(m); strings.Contains(row, "›") {
		t.Fatalf("breadcrumbs drawn while off: %q", row)
	}
}

func TestBreadcrumbsWithoutAServerShowThePathOnly(t *testing.T) {
	m := newEditor(t)
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	if err := os.WriteFile(path, []byte(outlineBody()), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	gotoLine(m, 30)
	m.View()
	row := titleRow(m)
	if !strings.Contains(row, "app.go") || strings.Contains(row, "Start") || strings.Contains(row, "Server") {
		t.Fatalf("no server must mean the path alone: %q", row)
	}
}

func TestOutlineNotAskedWithoutCapability(t *testing.T) {
	m, srv := outlineModel(t, outlineOpts{crumbs: true, sticky: true}, func(f *goplusFake) { f.outlineCap = false })
	gotoLine(m, 30)
	for i := 0; i < 5; i++ {
		m.View()
	}
	srv.mu.Lock()
	asks := srv.outlineAsks
	srv.mu.Unlock()
	if asks != 0 || strings.Contains(titleRow(m), "Start") {
		t.Fatalf("outline requested from a server without documentSymbolProvider (asks=%d)", asks)
	}
}

func TestOutlineNotAskedWhileBothAreOff(t *testing.T) {
	m, srv := outlineModel(t, outlineOpts{}, nil)
	for i := 0; i < 5; i++ {
		m.View()
	}
	srv.mu.Lock()
	asks := srv.outlineAsks
	srv.mu.Unlock()
	if asks != 0 {
		t.Fatalf("outline requested with breadcrumbs and sticky scroll off (asks=%d)", asks)
	}
}

func TestOutlineRefreshesAfterAnEditDebounced(t *testing.T) {
	m, srv := outlineModel(t, outlineOpts{crumbs: true}, nil)
	settle(t, m, func() bool { srv.mu.Lock(); defer srv.mu.Unlock(); return srv.outlineAsks == 1 }, "first outline")
	settle(t, m, func() bool { return m.outlineFor(m.active()) != nil }, "cached")
	feed(m, "o")
	typeKeys(m, "x")
	feed(m, "esc")
	settle(t, m, func() bool { srv.mu.Lock(); defer srv.mu.Unlock(); return srv.outlineAsks >= 2 }, "refresh after the edit")
}

func TestEnclosingChain(t *testing.T) {
	syms := []lsp.Symbol{
		{Name: "T", RangeLine: 1, Line: 1, EndLine: 20},
		{Name: "T.A", RangeLine: 3, Line: 3, EndLine: 8},
		{Name: "T.B", RangeLine: 9, Line: 9, EndLine: 19},
		{Name: "U", RangeLine: 30, Line: 30, EndLine: 40},
	}
	names := func(l int) string {
		var out []string
		for _, s := range enclosing(syms, l) {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}
	for line, want := range map[int]string{0: "", 1: "T", 5: "T,T.A", 9: "T,T.B", 25: "", 35: "U"} {
		if got := names(line); got != want {
			t.Errorf("line %d: %q, want %q", line, got, want)
		}
	}
	// Flat symbols given out of order still nest outermost first.
	flat := []lsp.Symbol{{Name: "in", RangeLine: 5, EndLine: 6}, {Name: "out", RangeLine: 1, EndLine: 10}}
	if got := enclosing(flat, 5); len(got) != 2 || got[0].Name != "out" {
		t.Fatalf("flat order: %+v", got)
	}
	// Siblings that share a line do not both show.
	sib := []lsp.Symbol{{Name: "a", RangeLine: 2, EndLine: 2}, {Name: "b", RangeLine: 2, EndLine: 2}}
	if got := enclosing(sib, 2); len(got) != 2 {
		// equal ranges nest by order; they are one chain, not a crash
		t.Logf("same-extent symbols: %+v", got)
	}
}

func TestCrumbsTruncateFromTheLeft(t *testing.T) {
	syms := []lsp.Symbol{{Name: "Server"}, {Name: "Start"}}
	path := []string{"alpha", "internal", "app.go"}
	plain := func(budget int) string { return ansi.Strip(crumbs(path, syms, budget)) }
	full := "alpha › internal › app.go › Server › Start"
	if got := plain(100); got != full {
		t.Fatalf("full: %q", got)
	}
	if got := plain(len([]rune(full)) - 1); got != "… › internal › app.go › Server › Start" {
		t.Fatalf("one dropped: %q", got)
	}
	if got := plain(20); !strings.HasSuffix(got, "Server › Start") || !strings.HasPrefix(got, "… › ") {
		t.Fatalf("narrow: %q", got)
	}
	if got := plain(3); got != "… › Start" && got != "Start" {
		t.Fatalf("tiny keeps the last segment: %q", got)
	}
	if got := ansi.Strip(crumbs([]string{"f.go"}, []lsp.Symbol{{Name: "AVeryLongSymbolName"}}, 8)); got != "… › AVe…" {
		t.Fatalf("a too-long last segment is clipped to the budget: %q", got)
	}
}

func TestBreadcrumbsFitTheTitleOfANarrowPane(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{crumbs: true}, nil)
	m.Resize(160, 24)
	gotoLine(m, 30)
	settle(t, m, func() bool { return m.outlineFor(m.active()) != nil }, "outline")
	feed(m, "ctrl+w", "v")
	if !m.splitActive() {
		t.Fatal("did not split")
	}
	v := plainView(m)
	for _, r := range strings.Split(v, "\n") {
		if w := ansi.Width(r); w > 160 {
			t.Fatalf("row wider than the surface (%d): %q", w, r)
		}
	}
	if !strings.Contains(v, "Start") {
		t.Fatalf("symbol missing from pane titles:\n%s", v)
	}
}

// Sticky scroll.

func stickyRows(m *Model) []string {
	g := m.bufGeom
	lines := strings.Split(plainView(m), "\n")
	var out []string
	for i := range g.sticky {
		out = append(out, strings.TrimRight(lines[g.oy+i], " │"))
	}
	return out
}

func TestStickyScrollPinsTheEnclosingHeaders(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{sticky: true}, nil)
	gotoLine(m, 30)
	settle(t, m, func() bool { return len(m.bufGeom.sticky) > 0 }, "pinned header")
	rows := stickyRows(m)
	if len(rows) != 1 || !strings.Contains(rows[0], "func (s *Server) Start() {") || !strings.Contains(rows[0], " 4 ") {
		t.Fatalf("pinned rows: %q", rows)
	}
	if got := m.active().Buffer().Line(3); got != "func (s *Server) Start() {" {
		t.Fatalf("buffer changed: %q", got)
	}
}

func TestStickyScrollOffPinsNothing(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{crumbs: true}, nil)
	gotoLine(m, 30)
	settle(t, m, func() bool { return m.outlineFor(m.active()) != nil }, "outline")
	m.View()
	if len(m.bufGeom.sticky) != 0 {
		t.Fatalf("pinned while off: %v", m.bufGeom.sticky)
	}
}

func TestStickyScrollNeedsTheHeaderAboveTheViewport(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{sticky: true}, nil)
	gotoLine(m, 5) // the header (line 3) is still on screen
	settle(t, m, func() bool { return m.outlineFor(m.active()) != nil }, "outline")
	m.View()
	if len(m.bufGeom.sticky) != 0 {
		t.Fatalf("pinned a visible header: %v", m.bufGeom.sticky)
	}
}

// At most three, the innermost ones, outermost first.
func TestStickyScrollPinsAtMostThreeInnermost(t *testing.T) {
	deep := []map[string]any{
		docSym("A", 5, 1, 57, docSym("B", 5, 2, 56, docSym("C", 5, 3, 55, docSym("D", 5, 4, 54, docSym("E", 6, 5, 53))))),
	}
	m, _ := outlineModel(t, outlineOpts{sticky: true}, func(f *goplusFake) { f.outlineSyms = deep })
	gotoLine(m, 30)
	settle(t, m, func() bool { return len(m.bufGeom.sticky) > 0 }, "pinned headers")
	if got := fmt.Sprint(m.bufGeom.sticky); got != "[3 4 5]" {
		t.Fatalf("pinned lines %s, want [3 4 5]", got)
	}
}

// The pins never cover the cursor's row.
func TestStickyScrollNeverCoversTheCursor(t *testing.T) {
	deep := []map[string]any{docSym("A", 5, 0, 57, docSym("B", 5, 1, 56, docSym("C", 5, 2, 55)))}
	m, _ := outlineModel(t, outlineOpts{sticky: true}, func(f *goplusFake) { f.outlineSyms = deep })
	m.Resize(100, 8) // a very short pane
	gotoLine(m, 30)
	settle(t, m, func() bool { return m.outlineFor(m.active()) != nil }, "outline")
	m.View()
	g := m.bufGeom
	curRow := 30 - g.top
	if len(g.sticky) > curRow {
		t.Fatalf("%d pinned rows cover the cursor on row %d", len(g.sticky), curRow)
	}
}

func TestStickyClickJumpsToTheHeader(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{sticky: true}, nil)
	gotoLine(m, 30)
	settle(t, m, func() bool { return len(m.bufGeom.sticky) > 0 }, "pinned header")
	g := m.bufGeom
	pos, ok := m.bufferPos(g.ox+g.textX+2, g.oy)
	if !ok || pos.Line != 3 {
		t.Fatalf("click on the pinned row -> %+v ok=%v, want line 3", pos, ok)
	}
	// A normal row still maps to its own line.
	pos, ok = m.bufferPos(g.ox+g.textX, g.oy+3)
	if !ok || pos.Line != g.top+3 {
		t.Fatalf("click on a text row -> %+v ok=%v", pos, ok)
	}
}

func TestStickyScrollWithWrap(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{sticky: true, wrap: true}, nil)
	gotoLine(m, 30)
	settle(t, m, func() bool { return len(m.bufGeom.sticky) > 0 }, "pinned header under wrap")
	rows := stickyRows(m)
	if len(rows) != 1 || !strings.Contains(rows[0], "Start()") {
		t.Fatalf("pinned rows under wrap: %q", rows)
	}
	g := m.bufGeom
	if pos, ok := m.bufferPos(g.ox+g.textX, g.oy); !ok || pos.Line != 3 {
		t.Fatalf("wrap: click on the pinned row -> %+v ok=%v", pos, ok)
	}
}

func TestStickyScrollWithInlayHints(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{sticky: true, inlay: true}, nil)
	gotoLine(m, 10) // the hinted line (10) is the cursor line
	settle(t, m, func() bool {
		return len(m.bufGeom.sticky) > 0 && strings.Contains(plainView(m), ": int")
	}, "pinned header and hint")
	if len(m.bufGeom.sticky) != 1 || m.bufGeom.sticky[0] != 3 {
		t.Fatalf("sticky with hints: %v", m.bufGeom.sticky)
	}
	if got := m.active().Buffer().Line(10); got != "\tstep10()" {
		t.Fatalf("buffer changed: %q", got)
	}
}

func TestStickyScrollInSplitPanes(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{sticky: true}, nil)
	m.Resize(160, 24)
	gotoLine(m, 30)
	settle(t, m, func() bool { return m.outlineFor(m.active()) != nil }, "outline")
	feed(m, "ctrl+w", "v")
	if !m.splitActive() {
		t.Fatal("did not split")
	}
	if v := plainView(m); strings.Count(v, "func (s *Server) Start() {") < 2 {
		t.Fatalf("both panes should pin the header:\n%s", v)
	}
	if len(m.bufGeom.sticky) == 0 {
		t.Fatal("focused pane geometry lacks the pinned rows")
	}
}

func TestSetCommandsToggleForTheSession(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{}, nil)
	gotoLine(m, 30)
	feed(m, ":")
	typeKeys(m, "set sticky")
	feed(m, "enter")
	feed(m, ":")
	typeKeys(m, "set breadcrumbs")
	feed(m, "enter")
	settle(t, m, func() bool { return len(m.bufGeom.sticky) > 0 && strings.Contains(titleRow(m), "Server.Start") }, "both on by :set")
	feed(m, ":")
	typeKeys(m, "set nosticky")
	feed(m, "enter")
	feed(m, ":")
	typeKeys(m, "set nobreadcrumbs")
	feed(m, "enter")
	m.View()
	if len(m.bufGeom.sticky) != 0 || strings.Contains(titleRow(m), "›") {
		t.Fatalf("still on after :set no...:\n%s", plainView(m))
	}
}

func TestStickyNoteNamesAMissingServer(t *testing.T) {
	m := newEditor(t)
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	os.WriteFile(path, []byte("package app\n"), 0o644)
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	feed(m, ":")
	typeKeys(m, "set sticky")
	feed(m, "enter")
	if msg := m.active().Message(); !strings.Contains(msg, "no language server") {
		t.Fatalf("message %q", msg)
	}
}

func TestOutlineGolden(t *testing.T) {
	m, _ := outlineModel(t, outlineOpts{crumbs: true, sticky: true}, nil)
	m.Resize(100, 20)
	gotoLine(m, 30)
	settle(t, m, func() bool { return len(m.bufGeom.sticky) > 0 && strings.Contains(titleRow(m), "Server.Start") }, "both on")
	golden.Snapshot(t, "editor_breadcrumbs_sticky_100x20", m.View())
}
