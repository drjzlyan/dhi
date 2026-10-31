package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cursorTo puts the tree cursor on path (expanding to it).
func cursorTo(t *testing.T, m *Model, path string) {
	t.Helper()
	m.revealPath(path)
	if n := m.cursorNode(); n == nil || n.path != path {
		t.Fatalf("cursor not on %s", path)
	}
}

// F-119: a new file in the folder under the cursor, then rename, then
// delete to DHI's trash — the tree and open buffers follow.
func TestTreeNewRenameDelete(t *testing.T) {
	m := newEditor(t)
	src := filepath.Join(m.ws.Root, "repos", "alpha", "src")
	cursorTo(t, m, src)
	feed(m, "a")
	typeKeys(m, "pkg/new.go")
	feed(m, "enter")
	created := filepath.Join(src, "pkg", "new.go")
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("not created: %v", err)
	}
	if m.active() == nil || m.active().Path() != created {
		t.Fatal("new file not opened")
	}
	m.bufFocus = false
	cursorTo(t, m, created)
	feed(m, "r", "ctrl+u")
	typeKeys(m, "src/pkg/renamed.go")
	feed(m, "enter")
	renamed := filepath.Join(src, "pkg", "renamed.go")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("not renamed: %v", err)
	}
	if m.active().Path() != renamed || !strings.HasSuffix(m.openVPath, "renamed.go") {
		t.Fatalf("buffer did not follow: %s / %s", m.active().Path(), m.openVPath)
	}
	cursorTo(t, m, renamed)
	feed(m, "d")
	if !strings.Contains(plainView(m), "DHI's trash") {
		t.Fatalf("no confirm:\n%s", plainView(m))
	}
	feed(m, "y")
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Fatal("not deleted")
	}
	if len(m.bufs) != 0 {
		t.Fatal("buffer of the deleted file kept")
	}
	trash, _ := filepath.Glob(filepath.Join(m.ws.Root, ".dhi", "tmp", "trash", "*", "renamed.go"))
	if len(trash) != 1 {
		t.Fatal("not in DHI's trash")
	}
}

func TestTreeNewRefusesOutsideWorkspace(t *testing.T) {
	m := newEditor(t)
	cursorTo(t, m, filepath.Join(m.ws.Root, "repos", "alpha", "src"))
	feed(m, "a")
	typeKeys(m, "../../../../evil.txt")
	feed(m, "enter")
	if m.treeOp == nil || !strings.Contains(m.treeOp.err, "inside the workspace") {
		t.Fatalf("op = %+v", m.treeOp)
	}
}

// F-119: files made outside DHI appear without a restart.
func TestTreeRefreshShowsOutsideChanges(t *testing.T) {
	m := newEditor(t)
	alpha := filepath.Join(m.ws.Root, "repos", "alpha")
	cursorTo(t, m, filepath.Join(alpha, "app.go"))
	_ = os.WriteFile(filepath.Join(alpha, "fresh.go"), []byte("x"), 0o644)
	_ = os.MkdirAll(filepath.Join(alpha, ".github"), 0o755)
	m.Update(diskTickMsg{})
	v := plainView(m)
	if !strings.Contains(v, "fresh.go") || !strings.Contains(v, ".github") {
		t.Fatalf("tree not refreshed:\n%s", v)
	}
}

// F-119: :e opens (or creates) a path; :w <path> saves as and keeps
// editing the new file; an existing target needs :w!.
func TestEditAndSaveAs(t *testing.T) {
	m, path := openApp(t)
	e := m.active()
	runEx(m, "w copy.go")
	copyPath := filepath.Join(filepath.Dir(path), "copy.go")
	if b, err := os.ReadFile(copyPath); err != nil || string(b) != "package main\n" {
		t.Fatalf("save as: %q %v", b, err)
	}
	if m.active().Path() != copyPath {
		t.Fatal("not editing the copy")
	}
	runEx(m, "w app.go")
	if !strings.Contains(e.Message(), ":w!") {
		t.Fatalf("msg = %q", e.Message())
	}
	runEx(m, "e src/util/helper.go")
	if !strings.HasSuffix(m.active().Path(), filepath.Join("src", "util", "helper.go")) {
		t.Fatalf("e opened %s", m.active().Path())
	}
}

// F-119: closed files reopen, and the start page lists recent ones.
func TestRecentAndReopen(t *testing.T) {
	m, path := openApp(t)
	runEx(m, "q")
	if len(m.bufs) != 0 {
		t.Fatal("not closed")
	}
	v := plainView(m)
	if !strings.Contains(v, "RECENT") || !strings.Contains(v, "app.go") {
		t.Fatalf("recent missing:\n%s", v)
	}
	runEx(m, "reopen")
	if m.active() == nil || m.active().Path() != path {
		t.Fatal("not reopened")
	}
}
