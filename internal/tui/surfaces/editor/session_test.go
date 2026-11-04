package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func sessionOpts(on bool) Option {
	return WithPrefs(func() Prefs { p := DefaultPrefs; p.RestoreSession = on; return p })
}

// sessionFixture opens two files with a cursor and a split, then saves
// the session as a quit would.
func sessionFixture(t *testing.T) (*workspace.Workspace, string, string) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	a := filepath.Join(ws.Root, "repos", "alpha", "src", "main.go")
	if err := os.WriteFile(a, []byte("l0\nl1\nl2\nl3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := vpathAbs(t, ws, "beta/README.md")
	m := New("test", ws, sessionOpts(true))
	m.Resize(160, 30)
	m.OpenPaths(a, b)
	m.tabFor(a).ed.Buffer().SetCursor(textbuf.Pos{Line: 2, Col: 1})
	m.activeTab = m.indexOfTab(m.tabFor(a))
	m.openSplit()
	m.SaveSession()
	return ws, a, b
}

// TestSessionRestoreRoundTrip: tabs, cursors, the active tab and the split
// come back in a new editor on the same workspace (F-149).
func TestSessionRestoreRoundTrip(t *testing.T) {
	ws, a, b := sessionFixture(t)
	m := New("test", ws, sessionOpts(true))
	if len(m.bufs) != 2 || m.bufs[0].path != a || m.bufs[1].path != b {
		t.Fatalf("tabs not restored: %d", len(m.bufs))
	}
	if got := m.tabFor(a).ed.Buffer().Cursor(); got != (textbuf.Pos{Line: 2, Col: 1}) {
		t.Fatalf("cursor = %+v", got)
	}
	if m.active() != m.tabFor(a).ed {
		t.Fatal("active tab not restored")
	}
	if !m.splitActive() || m.bufs[m.splitTab].path != b {
		t.Fatalf("split not restored: split=%v tab=%d", m.split, m.splitTab)
	}
	if !m.bufFocus || m.toast != nil {
		t.Fatalf("focus=%v toast=%v", m.bufFocus, m.toast)
	}
}

// TestSessionRestoreOff: the Settings toggle stops both saving and restoring.
func TestSessionRestoreOff(t *testing.T) {
	ws, _, _ := sessionFixture(t)
	if m := New("test", ws, sessionOpts(false)); len(m.bufs) != 0 {
		t.Fatalf("restored %d tabs with the setting off", len(m.bufs))
	}
	off := New("test", ws, sessionOpts(false))
	if err := os.Remove(off.sessionPath()); err != nil {
		t.Fatal(err)
	}
	off.OpenPaths(vpathAbs(t, ws, "beta/README.md"))
	off.SaveSession()
	if _, err := os.Stat(off.sessionPath()); err == nil {
		t.Fatal("session written with the setting off")
	}
}

// TestSessionRestoreSkipsGoneAndLarge: missing files and files over the
// large-file threshold are skipped, never prompted, and named in one toast.
func TestSessionRestoreSkipsGoneAndLarge(t *testing.T) {
	ws, a, b := sessionFixture(t)
	big := filepath.Join(ws.Root, "repos", "alpha", "big.txt")
	if err := os.WriteFile(big, bytes.Repeat([]byte("a\n"), largeFileThreshold/2+1), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New("test", ws, sessionOpts(true))
	m.openNode(&node{kind: nodeFile, path: big, name: "big.txt"}, true)
	m.SaveSession()
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}

	r := New("test", ws, sessionOpts(true))
	if len(r.bufs) != 1 || r.bufs[0].path != a {
		t.Fatalf("want only %s, got %d tabs", a, len(r.bufs))
	}
	if r.pendingOpen != nil {
		t.Fatal("startup must not show the large-file prompt")
	}
	if r.splitActive() {
		t.Fatal("split must end when its buffer is gone")
	}
	if r.toast == nil || !strings.Contains(r.toast.Detail, "README.md") || !strings.Contains(r.toast.Detail, "big.txt") {
		t.Fatalf("toast = %+v", r.toast)
	}
}

// TestSessionRestoreKeepsRecentAndIgnoresCorrupt: restoring does not reorder
// the recent list, and a damaged file is reported, not trusted.
func TestSessionRestoreKeepsRecentAndIgnoresCorrupt(t *testing.T) {
	ws, _, _ := sessionFixture(t)
	probe := New("test", ws)
	before, _ := os.ReadFile(probe.recentFile())
	New("test", ws, sessionOpts(true))
	after, _ := os.ReadFile(probe.recentFile())
	if string(before) != string(after) {
		t.Fatalf("recent list changed by restore:\n%s\n--\n%s", before, after)
	}
	if err := os.WriteFile(probe.sessionPath(), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New("test", ws, sessionOpts(true))
	if len(m.bufs) != 0 || m.toast == nil || !strings.Contains(m.toast.Detail, "unreadable") {
		t.Fatalf("bufs=%d toast=%+v", len(m.bufs), m.toast)
	}
}
