package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// F-169: LSP inlay hints are virtual cells.

func typeHint(line, col int, label string) map[string]any {
	return map[string]any{"position": map[string]any{"line": line, "character": col}, "label": label, "kind": 1}
}

func paramHint(line, col int, label string) map[string]any {
	return map[string]any{"position": map[string]any{"line": line, "character": col}, "label": label, "kind": 2, "paddingRight": true}
}

// inlayHintsFor is the hints of "x := compute(1, 2)" on line 0.
func inlayHintsFor() []map[string]any {
	return []map[string]any{typeHint(0, 1, ": int"), paramHint(0, 13, "a:"), paramHint(0, 16, "b:")}
}

// inlayModel opens text as app.go against a fake server that provides
// hints; on turns editor.inlay_hints on.
func inlayModel(t *testing.T, text string, wrap, on bool, cfg func(*goplusFake)) (*Model, *goplusFake) {
	t.Helper()
	inlayDebounce = 0
	srv, mgr := startFakeServerCfg(t, "go", false, func(f *goplusFake) {
		f.inlayCap, f.inlayHints = true, inlayHintsFor()
		if cfg != nil {
			cfg(f)
		}
	})
	m := newEditor(t)
	m.lspMgr = mgr
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.active() == nil {
		t.Fatal("app.go did not open")
	}
	m.prefs = func() Prefs {
		return Prefs{TabWidth: 4, LineNumbers: true, Wrap: wrap, InlayHints: on, Scrollback: 1000}
	}
	return m, srv
}

// settle renders and applies async messages until cond holds.
func settle(t *testing.T, m *Model, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m.View()
		select {
		case msg := <-m.termMsgs:
			m.Update(msg)
		case <-time.After(5 * time.Millisecond):
		}
		if cond() {
			return
		}
	}
	t.Fatalf("timeout waiting for %s:\n%s", what, plainView(m))
}

func hasHints(m *Model) bool { return strings.Contains(plainView(m), "x: int := compute(a: 1, b: 2)") }

func TestInlayHintsRenderInlineWithoutChangingTheBuffer(t *testing.T) {
	m, _ := inlayModel(t, "x := compute(1, 2)\n", false, true, nil)
	settle(t, m, func() bool { return hasHints(m) }, "hints")
	if got := m.active().Buffer().Line(0); got != "x := compute(1, 2)" {
		t.Fatalf("buffer changed: %q", got)
	}
	if !strings.Contains(m.View(), "\x1b[") {
		t.Fatal("no styling")
	}
}

func TestInlayHintsDefaultOffAndNotRequested(t *testing.T) {
	m, srv := inlayModel(t, "x := compute(1, 2)\n", false, false, nil)
	for i := 0; i < 5; i++ {
		m.View()
		time.Sleep(10 * time.Millisecond)
	}
	srv.mu.Lock()
	asks := srv.inlayAsks
	srv.mu.Unlock()
	if asks != 0 || hasHints(m) || strings.Contains(plainView(m), ": int") {
		t.Fatalf("hints requested or drawn while off (asks=%d)", asks)
	}
}

func TestInlayHintsNotAskedWithoutServerSupport(t *testing.T) {
	m, srv := inlayModel(t, "x := compute(1, 2)\n", false, true, func(f *goplusFake) { f.inlayCap = false })
	for i := 0; i < 5; i++ {
		m.View()
		time.Sleep(10 * time.Millisecond)
	}
	srv.mu.Lock()
	asks := srv.inlayAsks
	srv.mu.Unlock()
	if asks != 0 || strings.Contains(plainView(m), ": int") {
		t.Fatalf("asked a server without inlayHintProvider (asks=%d)", asks)
	}
}

func TestInlaySetCommandTogglesForTheSession(t *testing.T) {
	m, _ := inlayModel(t, "x := compute(1, 2)\n", false, false, nil)
	feed(m, ":")
	typeKeys(m, "set inlayhints")
	feed(m, "enter")
	settle(t, m, func() bool { return hasHints(m) }, "hints after :set inlayhints")
	feed(m, ":")
	typeKeys(m, "set noinlayhints")
	feed(m, "enter")
	if strings.Contains(plainView(m), ": int") {
		t.Fatalf("hints still drawn after :set noinlayhints:\n%s", plainView(m))
	}
}

// Cursor motion and clicks address buffer columns; hints are skipped.
func TestInlayCursorAndClicksSkipHints(t *testing.T) {
	m, _ := inlayModel(t, "x := compute(1, 2)\n", false, true, nil)
	settle(t, m, func() bool { return hasHints(m) }, "hints")
	g := m.bufGeom
	cell := func(s string) int { // display cell of s within the drawn line
		row := strings.Split(plainView(m), "\n")[g.oy]
		i := strings.Index(row, s)
		if i < 0 {
			t.Fatalf("%q not in %q", s, row)
		}
		return len([]rune(row[:i]))
	}
	click := func(s string, off int) textbuf.Pos {
		p, ok := m.bufferPos(cell(s)+off, g.oy)
		if !ok {
			t.Fatalf("click on %q missed", s)
		}
		return p
	}
	if p := click("compute", 0); p.Col != 5 {
		t.Fatalf("click on compute -> col %d, want 5", p.Col)
	}
	if p := click("1, b:", 0); p.Col != 13 {
		t.Fatalf("click on 1 -> col %d, want 13", p.Col)
	}
	if p := click("a:", 0); p.Col != 13 { // on a hint: the rune it precedes
		t.Fatalf("click on the a: hint -> col %d, want 13", p.Col)
	}
	if p := click(": int", 1); p.Col != 1 {
		t.Fatalf("click on the type hint -> col %d, want 1 (the rune it precedes)", p.Col)
	}
	// Motion counts buffer runes only: 0, then l x 5 lands on "c".
	feed(m, "0", "l", "l", "l", "l", "l")
	if c := m.active().Buffer().Cursor(); c.Col != 5 {
		t.Fatalf("cursor col %d after 5 l, want 5", c.Col)
	}
	// The block sits on the "c" of compute, after the hint: the rune under
	// it is still the buffer's.
	if !strings.Contains(plainView(m), "x: int := compute") {
		t.Fatal("hints lost with the cursor on the line")
	}
}

// A selection covers buffer text only: the hints inside it are drawn but
// the selection range is the buffer's.
func TestInlaySelectionIgnoresHints(t *testing.T) {
	m, _ := inlayModel(t, "x := compute(1, 2)\n", false, true, nil)
	settle(t, m, func() bool { return hasHints(m) }, "hints")
	feed(m, "0", "v", "l", "l")
	if !hasHints(m) {
		t.Fatalf("hints lost in visual mode:\n%s", plainView(m))
	}
	a, z := textbuf.Order(m.active().VisualStart(), m.active().Buffer().Cursor())
	if a.Col != 0 || z.Col != 2 {
		t.Fatalf("selection %+v..%+v, want cols 0..2", a, z)
	}
}

// Wrap counts hints as width of their row but never as text.
func TestInlayWrapRowsFitAndClickMaps(t *testing.T) {
	line := "x := compute(1, 2) " + words(24)
	m, _ := inlayModel(t, line+"\n", true, true, nil)
	settle(t, m, func() bool { return hasHints(m) }, "hints")
	g := m.bufGeom
	if len(g.rowStart) < 2 {
		t.Fatalf("line did not wrap: %+v", g)
	}
	textW := m.bufTextWidth(len("1") + 1)
	for i, r := range strings.Split(plainView(m), "\n")[g.oy : g.oy+len(g.rowLine)] {
		if w := len([]rune(strings.TrimRight(string([]rune(r)[g.ox+g.textX:]), " "))); w > textW {
			t.Errorf("row %d is %d cells, text width %d: %q", i, w, textW, r)
		}
	}
	// Every buffer rune is drawn exactly once across the rows.
	v := strings.ReplaceAll(plainView(m), "\n", "")
	if strings.Count(v, "wordz") != strings.Count(line, "wordz") {
		t.Errorf("text lost or duplicated in wrap:\n%s", plainView(m))
	}
	// A click on the second row maps to a column inside the second segment.
	p, ok := m.bufferPos(g.ox+g.textX+3, g.oy+1)
	if !ok || p.Line != 0 || p.Col < g.rowStart[1] {
		t.Fatalf("click on row 2 = %+v ok=%v, want col >= %d", p, ok, g.rowStart[1])
	}
}

// Without hints the wrap breaker is unchanged.
func TestWrapSegmentsHintedMatchesPlainWithoutHints(t *testing.T) {
	plain := words(30)
	a, b := wrapSegments(plain, 20), wrapSegmentsHinted(plain, 20, nil)
	if len(a) != len(b) {
		t.Fatalf("%v vs %v", a, b)
	}
	// A hint before rune 0 narrows the first row.
	h := make([]int, len([]rune(plain))+1)
	h[0] = 6
	c := wrapSegmentsHinted(plain, 20, h)
	if c[1] >= a[1] {
		t.Fatalf("hint width ignored: %v vs %v", c, a)
	}
}

func TestDevirtCell(t *testing.T) {
	cells := []inlayCell{{idx: 1, cell: 1, w: 5}, {idx: 3, cell: 3, w: 2}}
	for v, want := range map[int]int{0: 0, 1: 1, 5: 1, 6: 1, 7: 2, 8: 3, 9: 3, 10: 3, 11: 4} {
		if got := devirtCell(cells, v); got != want {
			t.Errorf("devirtCell(%d) = %d, want %d", v, got, want)
		}
	}
}

// An answer for an older request, or an older buffer version, never lands.
func TestInlayStaleAnswerDropped(t *testing.T) {
	m, _ := inlayModel(t, "x := compute(1, 2)\n", false, true, nil)
	m.View()
	path := m.active().Path()
	st := m.inlay[path]
	if st == nil {
		t.Fatal("no request state")
	}
	st.gen += 5 // a newer request is already out
	m.applyInlayMsg(teaMsg{kind: lspMsgInlay, note: path, sigSeq: st.gen - 1, hoverOK: true})
	if st.have {
		t.Fatal("an answer from an older request was applied")
	}
}

// Edits hide the hints of the line being edited at once, keep the others
// until the new answer, and ask again.
func TestInlayEditHidesCursorLineHintsThenRefreshes(t *testing.T) {
	m, srv := inlayModel(t, "x := compute(1, 2)\n", false, true, nil)
	settle(t, m, func() bool { return hasHints(m) }, "hints")
	feed(m, "A")
	typeKeys(m, " ")
	if strings.Contains(plainView(m), ": int") {
		t.Fatalf("stale hints drawn on the edited line:\n%s", plainView(m))
	}
	feed(m, "esc")
	settle(t, m, func() bool { return hasHints(m) }, "hints again")
	srv.mu.Lock()
	asks := srv.inlayAsks
	srv.mu.Unlock()
	if asks < 2 {
		t.Fatalf("asks = %d, want a new request for the new version", asks)
	}
}

// Requests cover the window and a screen of margin, not the whole file.
func TestInlayRequestsOnlyTheWindow(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		sb.WriteString("y := f(1)\n")
	}
	m, srv := inlayModel(t, sb.String(), false, true, nil)
	settle(t, m, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.inlayAsks > 0
	}, "first request")
	srv.mu.Lock()
	rng := srv.inlayRanges[0]
	srv.mu.Unlock()
	if rng == "0-399" {
		t.Fatalf("asked for the whole file: %s", rng)
	}
}

func TestInlayGolden(t *testing.T) {
	m, _ := inlayModel(t, "x := compute(1, 2)\ny := x + 1\n", false, true, func(f *goplusFake) {
		f.inlayHints = append(inlayHintsFor(), typeHint(1, 1, ": int"))
	})
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 1, Col: 5})
	settle(t, m, func() bool { return strings.Contains(plainView(m), "y: int := x + 1") }, "hints")
	golden.Snapshot(t, "editor_inlay_100x30", m.View())
}

// F-166 x F-169: both panes of a split on the same file show the hints,
// and the focused pane's geometry carries them for clicks.
func TestInlayWorksInSplitPanes(t *testing.T) {
	m, _ := inlayModel(t, "x := compute(1, 2)\n", false, true, nil)
	m.Resize(160, 30)
	feed(m, "ctrl+w", "v")
	if !m.splitActive() {
		t.Fatal("did not split")
	}
	settle(t, m, func() bool { return strings.Count(plainView(m), "x: int := compute(a: 1, b: 2)") >= 2 }, "hints in both panes")
	if len(m.bufGeom.inlay[0]) != 3 {
		t.Fatalf("focused pane geometry lacks hints: %+v", m.bufGeom.inlay)
	}
}
