package editor

import (
	"strings"
	"testing"
)

// TestSplitPanes pins F-057: ctrl+w v shows two buffers side by side,
// ctrl+w w moves focus, :only and closing a buffer end the split, and a
// narrow window folds back to one pane.
func TestSplitPanes(t *testing.T) {
	m := newEditor(t)
	m.Resize(160, 30)
	ws := m.ws
	if m.OpenPaths(vpathAbs(t, ws, "alpha/app.go"), vpathAbs(t, ws, "beta/README.md")) != 2 {
		t.Fatal("open two files")
	}
	first := m.activeTab
	feed(m, "ctrl+w", "v")
	if !m.splitActive() {
		t.Fatal("ctrl+w v did not split")
	}
	v := plainView(m)
	if !strings.Contains(v, "app.go") || !strings.Contains(v, "# beta") {
		t.Fatalf("both buffers must show:\n%s", v)
	}
	feed(m, "ctrl+w", "w")
	if m.activeTab == first {
		t.Fatal("ctrl+w w did not move focus")
	}
	feed(m, "ctrl+w", "w")
	if m.activeTab != first {
		t.Fatal("ctrl+w w twice must come back")
	}

	// Too narrow: one pane, nothing overflows.
	m.Resize(90, 30)
	if strings.Count(plainView(m), "╮") > 2 {
		t.Fatalf("narrow window still splits:\n%s", plainView(m))
	}
	m.Resize(160, 30)

	feed(m, ":", "o", "n", "l", "y", "enter")
	if m.splitActive() {
		t.Fatal(":only did not end the split")
	}

	feed(m, "ctrl+w", "v", ":", "q", "enter")
	if m.splitActive() || len(m.bufs) != 1 {
		t.Fatalf("closing a pane's buffer: split=%v bufs=%d", m.split, len(m.bufs))
	}
}
