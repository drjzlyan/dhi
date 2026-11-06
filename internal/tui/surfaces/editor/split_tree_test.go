package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// splitModel opens the four fixture files at 160x40 with app.go active.
func splitModel(t *testing.T) *Model {
	t.Helper()
	m := newEditor(t)
	m.Resize(160, 40)
	n := m.OpenPaths(
		vpathAbs(t, m.ws, "alpha/app.go"),
		vpathAbs(t, m.ws, "alpha/src/main.go"),
		vpathAbs(t, m.ws, "alpha/src/util/helper.go"),
		vpathAbs(t, m.ws, "beta/README.md"),
	)
	if n != 4 {
		t.Fatalf("opened %d files", n)
	}
	m.activeTab = m.indexOfTab(m.tabFor(vpathAbs(t, m.ws, "alpha/app.go")))
	return m
}

func focusedName(m *Model) string { return filepath.Base(m.bufs[m.activeTab].path) }

func leafNames(m *Model) []string {
	var out []string
	for _, l := range m.layout.leaves() {
		out = append(out, filepath.Base(l.tab.path))
	}
	return out
}

// TestSplitTreeThreeAndFourPanes pins F-166: nested vertical and horizontal
// splits, the pane cap, and snapshots of both layouts.
func TestSplitTreeThreeAndFourPanes(t *testing.T) {
	m := splitModel(t)
	feed(m, "ctrl+w", "v", "ctrl+w", "s") // app.go | previous tab; app.go over another
	if got := len(m.layout.leaves()); got != 3 {
		t.Fatalf("want 3 panes, got %d: %v", got, leafNames(m))
	}
	if m.layout.dir != paneCols || m.layout.kids[0].dir != paneRows {
		t.Fatalf("want cols[rows[a,b],c]: %+v", m.layout)
	}
	if focusedName(m) != "app.go" {
		t.Fatalf("focus moved on split: %s", focusedName(m))
	}
	golden.Snapshot(t, "editor_split3_160x40", m.View())

	feed(m, "ctrl+w", "l", "ctrl+w", "s") // stack the right-hand pane too
	if got := len(m.layout.leaves()); got != 4 {
		t.Fatalf("want 4 panes, got %d", got)
	}
	golden.Snapshot(t, "editor_split4_160x40", m.View())

	feed(m, "ctrl+w", "v") // the fifth is refused, said once
	if got := len(m.layout.leaves()); got != 4 {
		t.Fatalf("cap exceeded: %d panes", got)
	}
	if m.toast == nil || !strings.Contains(m.toast.Detail, "at most 4") {
		t.Fatalf("refusal not shown: %+v", m.toast)
	}

	// The rects tile the area exactly.
	x, w, h := m.mainBox()
	area := 0
	for _, r := range m.paneRects() {
		area += r.w * r.h
		if r.x < x || r.x+r.w > x+w || r.y+r.h > h {
			t.Fatalf("pane outside the box: %+v", r)
		}
	}
	if area != w*h {
		t.Fatalf("panes cover %d of %d cells", area, w*h)
	}
}

// TestSplitFocusByDirectionAndCycle: h j k l move to the neighbor in that
// direction, w / W cycle in reading order.
func TestSplitFocusByDirectionAndCycle(t *testing.T) {
	m := splitModel(t)
	feed(m, "ctrl+w", "v", "ctrl+w", "s") // [A over B] | C ; focus A (top-left)
	names := leafNames(m)
	a, b, c := names[0], names[1], names[2]
	feed(m, "ctrl+w", "j")
	if focusedName(m) != b {
		t.Fatalf("j: want %s, got %s", b, focusedName(m))
	}
	feed(m, "ctrl+w", "k")
	if focusedName(m) != a {
		t.Fatalf("k: want %s, got %s", a, focusedName(m))
	}
	feed(m, "ctrl+w", "l")
	if focusedName(m) != c {
		t.Fatalf("l: want %s, got %s", c, focusedName(m))
	}
	feed(m, "ctrl+w", "h")
	if focusedName(m) != a {
		t.Fatalf("h: want %s, got %s", a, focusedName(m))
	}
	feed(m, "ctrl+w", "h") // nothing to the left: stays
	if focusedName(m) != a {
		t.Fatalf("h at the edge moved focus to %s", focusedName(m))
	}
	feed(m, "ctrl+w", "w")
	if focusedName(m) != b {
		t.Fatalf("w: want %s, got %s", b, focusedName(m))
	}
	feed(m, "ctrl+w", "W")
	if focusedName(m) != a {
		t.Fatalf("W: want %s, got %s", a, focusedName(m))
	}
}

// TestSplitClosePaneOnlyEqualize: c / q close the focused pane, o keeps
// only it, = changes nothing.
func TestSplitClosePaneOnlyEqualize(t *testing.T) {
	m := splitModel(t)
	feed(m, "ctrl+w", "v", "ctrl+w", "s")
	feed(m, "ctrl+w", "=")
	if len(m.layout.leaves()) != 3 {
		t.Fatal("= must not change the layout")
	}
	feed(m, "ctrl+w", "c") // closes the top-left pane; the stacked one remains
	if m.splitActive() && len(m.layout.leaves()) != 2 {
		t.Fatalf("close: %d panes", len(m.layout.leaves()))
	}
	if len(m.layout.leaves()) != 2 || m.layout.dir != paneCols {
		t.Fatalf("after close want a plain two-pane split: %+v", m.layout)
	}
	if len(m.bufs) != 4 {
		t.Fatalf("closing a pane closed a buffer: %d tabs", len(m.bufs))
	}
	feed(m, "ctrl+w", "q")
	if m.splitActive() {
		t.Fatal("ctrl+w q on the second-to-last pane must end the split")
	}
	feed(m, "ctrl+w", "v", "ctrl+w", "s", "ctrl+w", "o")
	if m.splitActive() {
		t.Fatal("ctrl+w o did not keep one pane")
	}
	feed(m, ":", "s", "p", "enter", ":", "v", "s", "enter")
	if len(m.layout.leaves()) != 3 {
		t.Fatalf(":sp :vs gave %d panes", len(m.layout.leaves()))
	}
	feed(m, ":", "c", "l", "o", "s", "e", "enter")
	if len(m.layout.leaves()) != 2 {
		t.Fatalf(":close left %d panes", len(m.layout.leaves()))
	}
}

// TestSplitRefusedWhenTooSmall: a split that would leave a pane under the
// minimum is refused with a note; a layout that no longer fits folds.
func TestSplitRefusedWhenTooSmall(t *testing.T) {
	m := splitModel(t)
	m.Resize(110, 40)
	feed(m, "ctrl+w", "v") // 110-rail leaves room for two columns only
	panes := len(m.layout.leaves())
	feed(m, "ctrl+w", "v")
	if len(m.layout.leaves()) != panes {
		t.Fatalf("an undersized split was taken: %d panes", len(m.layout.leaves()))
	}
	if m.toast == nil || !strings.Contains(m.toast.Detail, "no room") {
		t.Fatalf("no refusal shown: %+v", m.toast)
	}
	m.Resize(160, 12) // too short for a stacked pair
	m.toast = nil
	if m.splitActive() {
		feed(m, "ctrl+w", "o")
	}
	feed(m, "ctrl+w", "s")
	if m.splitActive() {
		t.Fatal("stacked split taken in a 12-row window")
	}
}

// TestSplitCloseBufferKeepsOtherPanes: closing a tab removes its panes only.
func TestSplitCloseBufferKeepsOtherPanes(t *testing.T) {
	m := splitModel(t)
	feed(m, "ctrl+w", "v", "ctrl+w", "s")
	feed(m, ":", "q", "enter") // closes the focused pane's buffer
	if len(m.bufs) != 3 {
		t.Fatalf("tabs = %d", len(m.bufs))
	}
	if !m.splitActive() || len(m.layout.leaves()) != 2 {
		t.Fatalf("want 2 panes after closing one pane's buffer: %+v", m.layout)
	}
	for _, l := range m.layout.leaves() {
		if m.indexOfTab(l.tab) < 0 {
			t.Fatal("a pane shows a closed tab")
		}
	}
	if m.bufs[m.activeTab] != m.focus.tab {
		t.Fatal("focus and active tab disagree")
	}
}

// TestSplitSameBufferTwice: with one buffer a split shows it twice; the
// cursor is the buffer's, shared by both panes (documented limit).
func TestSplitSameBufferTwice(t *testing.T) {
	m := newEditor(t)
	m.Resize(160, 30)
	m.OpenPaths(vpathAbs(t, m.ws, "alpha/app.go"))
	feed(m, "ctrl+w", "v", "ctrl+w", "s")
	ls := m.layout.leaves()
	if len(ls) != 3 || ls[0].tab != ls[1].tab || ls[1].tab != ls[2].tab {
		t.Fatalf("want one tab in three panes: %v", leafNames(m))
	}
	if v := plainView(m); strings.Count(v, "package main") < 3 {
		t.Fatalf("every pane must draw the buffer:\n%s", v)
	}
}

// TestSplitClickFocusesPane: a click in another pane focuses it and
// places the cursor there.
func TestSplitClickFocusesPane(t *testing.T) {
	m := splitModel(t)
	feed(m, "ctrl+w", "v", "ctrl+w", "s")
	m.View()
	var target *paneRect
	for i := range m.panes {
		if m.panes[i].leaf != m.focus && m.panes[i].x > m.railW()+m.width/3 {
			target = &m.panes[i]
		}
	}
	if target == nil {
		t.Fatal("no right-hand pane")
	}
	before := m.focus
	if !m.Click(target.geom.ox+target.geom.textX+2, target.geom.oy) {
		t.Fatal("click in another pane not handled")
	}
	if m.focus == before || m.focus != target.leaf {
		t.Fatal("click did not focus the pane under it")
	}
	if m.bufs[m.activeTab] != target.leaf.tab {
		t.Fatal("active tab is not the clicked pane's")
	}
}

// TestSplitPanesRenderPerBuffer: wrap uses the pane's width and a block
// selection draws in its own pane.
func TestSplitPanesRenderPerBuffer(t *testing.T) {
	m := splitModel(t)
	m.prefs = func() Prefs { return Prefs{TabWidth: 4, LineNumbers: true, Wrap: true, Scrollback: 1000} }
	long := "package main // " + words(40) + "\n"
	app := m.tabFor(vpathAbs(t, m.ws, "alpha/app.go"))
	app.ed.Buffer().SetText(long + "second\nthird\n")
	feed(m, "ctrl+w", "v", "ctrl+w", "s")
	feed(m, "ctrl+v", "j", "l")
	if !m.active().VisualBlock() {
		t.Fatal("want block visual")
	}
	view := m.View()
	for i, l := range strings.Split(plainView(m), "\n") {
		if w := len([]rune(l)); w > 160 {
			t.Fatalf("row %d is %d cells wide", i, w)
		}
	}
	// the wrapped long line breaks inside a pane a third of the width
	if g := m.bufGeom; len(g.rowLine) < 2 || g.rowLine[0] != g.rowLine[1] {
		t.Fatalf("focused pane did not wrap its long line: %v", g.rowLine)
	}
	golden.AssertBgIntegrity(t, "split wrap block", view)
}

// TestSessionRestoresPaneTree: a three-pane layout survives quit and
// relaunch; a session with only the old two-pane field still restores.
func TestSessionRestoresPaneTree(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	a := filepath.Join(ws.Root, "repos", "alpha", "app.go")
	b := vpathAbs(t, ws, "beta/README.md")
	c := filepath.Join(ws.Root, "repos", "alpha", "src", "main.go")
	m := New("test", ws, sessionOpts(true))
	m.Resize(160, 40)
	m.OpenPaths(a, b, c)
	m.activeTab = m.indexOfTab(m.tabFor(a))
	m.openSplit()
	feed(m, "ctrl+w", "l", "ctrl+w", "s") // focus the right pane and stack under it
	if len(m.layout.leaves()) != 3 {
		t.Fatalf("setup: %d panes", len(m.layout.leaves()))
	}
	wantFocus := m.bufs[m.activeTab].path
	m.SaveSession()

	r := New("test", ws, sessionOpts(true))
	if !r.splitActive() || len(r.layout.leaves()) != 3 {
		t.Fatalf("layout not restored: %+v", r.layout)
	}
	if r.layout.dir != paneCols || r.layout.kids[1].dir != paneRows {
		t.Fatalf("shape lost: %+v", r.layout)
	}
	if r.bufs[r.activeTab].path != wantFocus || r.focus.tab != r.bufs[r.activeTab] {
		t.Fatal("focused pane not restored")
	}

	// A session written before the pane tree.
	old := sessionFile{Version: sessionVersion, Active: a, Split: b,
		Tabs: []sessionTab{{Path: a}, {Path: b}}}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(r.sessionPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	o := New("test", ws, sessionOpts(true))
	if !o.splitActive() || len(o.layout.leaves()) != 2 || o.layout.dir != paneCols || o.bufs[o.activeTab].path != a {
		t.Fatalf("old split field not restored: %+v", o.layout)
	}

	// More panes than the cap is ignored rather than trusted.
	leaf := func(p string) sessionPane { return sessionPane{Path: a} }
	big := sessionFile{Version: sessionVersion, Active: a, Tabs: []sessionTab{{Path: a}},
		Layout: &sessionPane{Dir: "cols", Kids: []sessionPane{leaf(""), leaf(""), leaf(""), leaf(""), leaf("")}}}
	data, _ = json.Marshal(big)
	if err := os.WriteFile(r.sessionPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if g := New("test", ws, sessionOpts(true)); g.splitActive() {
		t.Fatal("a layout over the cap must be ignored")
	}
}
