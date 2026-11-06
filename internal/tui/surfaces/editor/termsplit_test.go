package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

// splitDrawer opens the drawer on a live editor and splits the first tab.
func splitDrawer(t *testing.T) *Model {
	t.Helper()
	m := liveEditor(t)
	feed(m, "ctrl+t")
	if len(m.terms) == 0 || m.terms[0].sess == nil {
		t.Skip("no session started")
	}
	feed(m, "alt+s")
	return m
}

func TestTermSplitOpensBesideInSameFolder(t *testing.T) {
	m := splitDrawer(t)
	if !m.termSplitActive() || len(m.terms) != 3 {
		t.Fatalf("split=%v tabs=%d", m.termSplitActive(), len(m.terms))
	}
	l, r := m.termIndex(m.termSplit[0]), m.termIndex(m.termSplit[1])
	if m.terms[l].dir != m.terms[r].dir || m.activeTerm != r {
		t.Errorf("new pane dir %q vs %q, active %d (want %d)", m.terms[r].dir, m.terms[l].dir, m.activeTerm, r)
	}
	if !strings.Contains(plainView(m), "▏") {
		t.Error("divider missing")
	}
	feed(m, "alt+s")
	if len(m.terms) != 3 {
		t.Error("a second alt+s must not open a third pane")
	}
}

func TestTermSplitPanesGetOwnSize(t *testing.T) {
	m := splitDrawer(t)
	lw, rw := m.paneWidths()
	for _, id := range m.termSplit {
		cols, _ := m.terms[m.termIndex(id)].screen.Size()
		if cols != lw && cols != rw {
			t.Errorf("pane cols = %d, want %d or %d", cols, lw, rw)
		}
	}
	if lw+rw+1 != m.width-4 {
		t.Errorf("panes %d+%d+divider != body %d", lw, rw, m.width-4)
	}
	m.Resize(140, 30)
	lw2, _ := m.paneWidths()
	if cols, _ := m.terms[m.termIndex(m.termSplit[0])].screen.Size(); cols != lw2 {
		t.Errorf("after resize cols = %d, want %d", cols, lw2)
	}
	feed(m, "alt+w") // back to one pane: the survivor is full width again
	if cols, _ := m.activeTermTab().screen.Size(); cols != m.width-4 {
		t.Errorf("single pane cols = %d, want %d", cols, m.width-4)
	}
}

func TestTermSplitFocusAndClose(t *testing.T) {
	m := splitDrawer(t)
	first := m.termIndex(m.termSplit[0])
	feed(m, "alt+o")
	if m.activeTerm != first {
		t.Fatalf("alt+o: active = %d, want %d", m.activeTerm, first)
	}
	feed(m, "alt+o")
	if m.activeTerm == first {
		t.Fatal("alt+o did not return to the right pane")
	}
	right := m.terms[m.activeTerm].id
	m.Update(teaMsg{kind: termMsgOut, tab: right, chunk: []byte("right-pane-text")})
	m.Update(teaMsg{kind: termMsgOut, tab: m.terms[first].id, chunk: []byte("left-pane-text")})
	v := plainView(m)
	if !strings.Contains(v, "left-pane-text") || !strings.Contains(v, "right-pane-text") {
		t.Fatalf("both panes must show:\n%s", v)
	}
	leftID := m.terms[first].id
	feed(m, "alt+w") // close the focused (right) pane
	if m.termSplitActive() || m.termSplit != [2]int{} || len(m.terms) != 2 {
		t.Fatalf("split not cleared: %v tabs=%d", m.termSplit, len(m.terms))
	}
	if m.terms[m.activeTerm].id != leftID {
		t.Error("the surviving pane is not active")
	}
}

func TestTermSplitFoldsWhenNarrow(t *testing.T) {
	m := liveEditor(t)
	m.Resize(40, 30)
	feed(m, "ctrl+t", "alt+s")
	if m.termSplitActive() || len(m.terms) != 2 {
		t.Errorf("a 40-column drawer must refuse the split (tabs=%d)", len(m.terms))
	}
}

func TestTermSplitHelpAndHints(t *testing.T) {
	m := newEditor(t)
	feed(m, "ctrl+t")
	_, hints := m.Hints()
	if !strings.Contains(strings.Join(hints, "|"), "alt+s split") {
		t.Errorf("hints = %v", hints)
	}
	var help string
	for _, s := range m.HelpSections() {
		help += s[0] + s[1]
	}
	if !strings.Contains(help, "alt+s") || !strings.Contains(help, "alt+o") {
		t.Error("help lacks the split keys")
	}
}

func TestTermSplitGolden(t *testing.T) {
	m := newEditor(t) // refused sessions render their message: deterministic
	m.Resize(100, 30)
	feed(m, "ctrl+t", "alt+s")
	if !m.termSplitActive() {
		t.Fatal("split did not open")
	}
	golden.Snapshot(t, "editor_drawer_split_100x30", plainView(m))
	feed(m, "alt+o")
	golden.Snapshot(t, "editor_drawer_split_left_100x30", plainView(m))
}
