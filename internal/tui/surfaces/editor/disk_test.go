package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/textbuf"
)

// openApp opens repos/alpha/app.go through the tree.
func openApp(t *testing.T) (*Model, string) {
	t.Helper()
	m := newEditor(t)
	path := filepath.Join(m.ws.Root, "repos", "alpha", "app.go")
	m.open(&node{kind: nodeFile, path: path, name: "app.go"})
	if m.active() == nil {
		t.Fatal("app.go did not open")
	}
	return m, path
}

// F-115: a clean buffer follows the file; a dirty one is warned once and
// :w refuses to clobber the outside change.
func TestDiskChangeReloadsCleanBuffer(t *testing.T) {
	m, path := openApp(t)
	if err := os.WriteFile(path, []byte("package main\n\nfunc x() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Update(diskTickMsg{})
	if got := m.active().Buffer().Text(); !strings.Contains(got, "func x") {
		t.Fatalf("clean buffer not reloaded: %q", got)
	}
	if !strings.Contains(m.active().Message(), "reloaded") {
		t.Fatalf("msg = %q", m.active().Message())
	}
}

func TestDiskChangeWarnsDirtyBufferAndSaveRefuses(t *testing.T) {
	m, path := openApp(t)
	e := m.active()
	e.Key("A")
	e.Key("!")
	e.Key("esc")
	_ = os.WriteFile(path, []byte("package other\n"), 0o644)
	m.Update(diskTickMsg{})
	if !strings.Contains(e.Message(), "changed on disk") {
		t.Fatalf("msg = %q", e.Message())
	}
	if !strings.Contains(e.Buffer().Text(), "!") {
		t.Fatal("dirty edits dropped by the poll")
	}
	e.SetMessage("")
	m.Update(diskTickMsg{}) // warned once per change
	if e.Message() != "" {
		t.Fatalf("warned again: %q", e.Message())
	}
	for _, k := range []string{":", "w", "enter"} {
		e.Key(k)
	}
	if got, _ := os.ReadFile(path); string(got) != "package other\n" {
		t.Fatalf(":w clobbered the outside change: %q", got)
	}
}

func TestUnsavedListsDirtyBuffersAndCtrlCLeavesInsert(t *testing.T) {
	m, path := openApp(t)
	if len(m.Unsaved()) != 0 {
		t.Fatal("clean buffer listed")
	}
	m.HandleKey("i")
	if !m.TakesCtrlC() {
		t.Fatal("insert mode must take ctrl+c")
	}
	m.HandleKey("x")
	m.HandleKey("ctrl+c")
	if m.active().Mode() != textbuf.ModeNormal || m.TakesCtrlC() {
		t.Fatal("ctrl+c did not leave insert mode")
	}
	if got := m.Unsaved(); len(got) != 1 || !strings.HasSuffix(got[0], "app.go") {
		t.Fatalf("unsaved = %v", got)
	}
	if err := m.SaveAll(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "xpackage main\n" {
		t.Fatalf("saved %q", b)
	}
}

// F-116: tab width and line numbers come from Settings, live.
func TestPrefsApplyToTheBufferView(t *testing.T) {
	m, path := openApp(t)
	_ = path
	prefs := Prefs{TabWidth: 8, LineNumbers: true, Scrollback: 1000}
	m.prefs = func() Prefs { return prefs }
	e := m.active()
	for _, k := range []string{"O", "tab", "x", "esc"} {
		e.Key(k)
	}
	if !strings.Contains(plainView(m), "1         x") { // "1 " gutter + 8-cell tab
		t.Fatalf("tab width 8 not applied:\n%s", plainView(m))
	}
	prefs.LineNumbers = false
	if strings.Contains(plainView(m), " 1 ") || strings.Contains(plainView(m), " 2 package") {
		t.Fatalf("line numbers still shown:\n%s", plainView(m))
	}
}

// F-116: the palette's Save saves even from insert mode.
func TestPaletteSaveFromInsertMode(t *testing.T) {
	m, path := openApp(t)
	m.HandleKey("i")
	m.HandleKey("z")
	for _, c := range m.Commands() {
		if c.Title == "Save file" {
			c.Run()
		}
	}
	if b, _ := os.ReadFile(path); string(b) != "zpackage main\n" {
		t.Fatalf("saved %q", b)
	}
	if strings.Contains(m.active().Buffer().Text(), ":w") {
		t.Fatal(":w typed into the buffer")
	}
}
