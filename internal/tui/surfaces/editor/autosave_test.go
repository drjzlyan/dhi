package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/textbuf"
)

// autosaveEditor opens app.go with a fake clock and the given mode.
func autosaveEditor(t *testing.T, mode string) (*Model, string, *time.Time) {
	t.Helper()
	m, path := openApp(t)
	clk := time.Unix(1000, 0)
	m.now = func() time.Time { return clk }
	m.prefs = func() Prefs { p := DefaultPrefs; p.Autosave = mode; return p }
	return m, path, &clk
}

func diskText(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAutosaveOffNeverSaves(t *testing.T) {
	m, path, clk := autosaveEditor(t, "off")
	feed(m, "A", "!", "esc")
	*clk = clk.Add(time.Minute)
	m.Update(autosaveTickMsg{})
	if strings.Contains(diskText(t, path), "!") || !m.active().Buffer().Dirty() {
		t.Fatal("off saved")
	}
}

func TestAutosaveDelayWaitsForQuiet(t *testing.T) {
	m, path, clk := autosaveEditor(t, "delay")
	feed(m, "A", "!") // still in insert mode
	*clk = clk.Add(autosaveDelay - time.Millisecond)
	m.Update(autosaveTickMsg{})
	if strings.Contains(diskText(t, path), "!") {
		t.Fatal("saved before the delay passed")
	}
	feed(m, "?") // a key restarts the wait
	*clk = clk.Add(autosaveDelay - time.Millisecond)
	m.Update(autosaveTickMsg{})
	if strings.Contains(diskText(t, path), "?") {
		t.Fatal("saved mid-typing")
	}
	*clk = clk.Add(time.Millisecond)
	m.Update(autosaveTickMsg{})
	if got := diskText(t, path); !strings.Contains(got, "!?") {
		t.Fatalf("not saved after the quiet delay: %q", got)
	}
	if m.active().Buffer().Dirty() {
		t.Fatal("still dirty after autosave")
	}
}

func TestAutosaveRunsSaveHooks(t *testing.T) {
	m, _, clk := autosaveEditor(t, "delay")
	saved := 0
	m.active().SetAfterSave(func(*textbuf.Editor) { saved++ })
	feed(m, "A", "!", "esc")
	*clk = clk.Add(2 * autosaveDelay)
	m.Update(autosaveTickMsg{})
	if saved != 1 {
		t.Fatalf("after-save hook ran %d times, want 1 (same path as :w)", saved)
	}
}

func TestAutosaveStopsWhenChangedOnDisk(t *testing.T) {
	m, path, clk := autosaveEditor(t, "delay")
	feed(m, "A", "!", "esc")
	if err := os.WriteFile(path, []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	*clk = clk.Add(2 * autosaveDelay)
	m.Update(autosaveTickMsg{})
	if diskText(t, path) != "package other\n" {
		t.Fatal("autosave overwrote an outside change")
	}
	if !strings.Contains(m.active().Message(), "autosave stopped") {
		t.Fatalf("msg = %q", m.active().Message())
	}
	m.active().SetMessage("")
	m.Update(autosaveTickMsg{}) // stays stopped, says nothing more
	if m.active().Message() != "" || diskText(t, path) != "package other\n" {
		t.Fatal("autosave retried after stopping")
	}
	feed(m, ":", "w", "!", "enter") // the user decides
	if !strings.Contains(diskText(t, path), "!") {
		t.Fatalf(":w! did not write: %q", diskText(t, path))
	}
	feed(m, "A", "x", "esc")
	*clk = clk.Add(2 * autosaveDelay)
	m.Update(autosaveTickMsg{}) // clears the stop (buffer was clean)
	feed(m, "A", "y", "esc")
	*clk = clk.Add(2 * autosaveDelay)
	m.Update(autosaveTickMsg{})
	if m.active().Buffer().Dirty() {
		t.Fatal("autosave did not resume once the buffer was clean")
	}
}

func TestAutosaveFocusSavesOnLeavingBuffer(t *testing.T) {
	m, path, clk := autosaveEditor(t, "focus")
	feed(m, "A", "!", "esc")
	*clk = clk.Add(time.Hour)
	m.Update(autosaveTickMsg{}) // time alone does nothing in focus mode
	if strings.Contains(diskText(t, path), "!") {
		t.Fatal("focus mode saved on a timer")
	}
	m.bufFocus = false // focus moves to the tree
	m.autosaveFocusChange(m.focusedTab(), true)
	if !strings.Contains(diskText(t, path), "!") {
		t.Fatal("not saved when the buffer lost focus")
	}
}

func TestAutosaveFocusSavesOnTabSwitch(t *testing.T) {
	m, path, _ := autosaveEditor(t, "focus")
	other := filepath.Join(m.ws.Root, "repos", "alpha", "other.txt")
	if err := os.WriteFile(other, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := m.focusedTab()
	feed(m, "A", "!", "esc")
	m.open(&node{kind: nodeFile, path: other, name: "other.txt"})
	m.autosaveFocusChange(first, true)
	if !strings.Contains(diskText(t, path), "!") {
		t.Fatal("not saved on tab switch")
	}
}

func TestAutosaveSkipsCommitMessage(t *testing.T) {
	m, _, clk := autosaveEditor(t, "delay")
	p := m.commitPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.open(&node{kind: nodeFile, path: p, name: "COMMIT_EDITMSG"})
	feed(m, "i", "h", "i", "esc")
	*clk = clk.Add(time.Hour)
	m.Update(autosaveTickMsg{})
	if diskText(t, p) != "\n" {
		t.Fatalf("commit message autosaved: %q", diskText(t, p))
	}
}

func TestAutosaveSkipsUnboundBuffer(t *testing.T) {
	m, path, clk := autosaveEditor(t, "delay")
	feed(m, "A", "!", "esc")
	m.active().SetPath("")
	m.focusedTab().path = ""
	*clk = clk.Add(time.Hour)
	m.Update(autosaveTickMsg{})
	if strings.Contains(diskText(t, path), "!") || !m.active().Buffer().Dirty() {
		t.Fatal("untitled buffer saved")
	}
}
