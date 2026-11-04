package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pumpCmd runs cmd and feeds every message it yields back through Update,
// following follow-up commands, as the bubbletea loop would.
func pumpCmd(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			pumpCmd(m, c)
		}
	default:
		pumpCmd(m, m.Update(msg))
	}
}

func wsymEditor(t *testing.T) (*Model, *goplusFake, string) {
	t.Helper()
	m, srv := symEditor(t)
	file := filepath.Join(m.members[0].path, "src", "main.go")
	if err := os.WriteFile(file, []byte("package main\n"+strings.Repeat("// line\n", 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.wsSyms = []map[string]any{
		wsSym("LoadConfig", 12, fileURI(file), 6),
		wsSym("Loader", 12, fileURI(file), 9),
		wsSym("Println", 12, "file:///usr/local/go/src/fmt/print.go", 1),
	}
	feed(m, "enter", "down", "enter", "down", "down", "enter") // open main.go: starts the server
	waitFor(t, m, func() bool { return len(m.lspMgr.Live()) > 0 }, "live server")
	return m, srv, file
}

func openWsym(t *testing.T, m *Model, cmd string) {
	t.Helper()
	feed(m, "esc", ":")
	typeKeys(m, cmd)
	feed(m, "enter")
	if m.mode != modeWorkspaceSyms {
		t.Fatalf("mode = %v, msg = %q", m.mode, m.active().Message())
	}
	pumpCmd(m, m.TakeCmd())
}

func TestWsymPickerListsMembersAndOpensFile(t *testing.T) {
	m, _, file := wsymEditor(t)
	openWsym(t, m, "wsym load")
	out := plainView(m)
	for _, want := range []string{"workspace symbols", "pkg.LoadConfig", "pkg.Loader", "src/main.go:7"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Println") {
		t.Errorf("symbol outside every member leaked:\n%s", out)
	}
	feed(m, "down", "enter")
	if m.mode != modeNav || m.wsym != nil {
		t.Fatal("picker should close after opening")
	}
	if e := m.active(); e == nil || e.Path() != file || e.Buffer().Cursor().Line != 9 {
		t.Fatalf("active = %v", e)
	}
}

func TestWsymTypingRequeriesAndRanksLocally(t *testing.T) {
	m, _, _ := wsymEditor(t)
	openWsym(t, m, "wsym")
	if len(m.wsym.shown) != 2 {
		t.Fatalf("shown = %d, want 2 (server order)", len(m.wsym.shown))
	}
	seq := m.wsym.seq
	typeKeys(m, "ldr") // fuzzy: matches Loader only
	if got := m.wsym.shown; len(got) != 1 || got[0].sym.Name != "Loader" {
		t.Fatalf("local rank = %+v", got)
	}
	if m.wsym.seq != seq+3 || !m.wsym.waiting {
		t.Fatalf("seq=%d waiting=%v: each key must start a new query generation", m.wsym.seq, m.wsym.waiting)
	}
	// Debounce: ticks for superseded generations do nothing; the last one queries.
	if cmd := m.Update(wsymTickMsg{seq: seq + 1}); cmd != nil {
		t.Fatal("stale tick ran a query")
	}
	pumpCmd(m, m.Update(wsymTickMsg{seq: seq + 3}))
	if m.wsym.waiting {
		t.Fatal("answer not applied")
	}
}

func TestWsymDropsStaleReplies(t *testing.T) {
	m, _, _ := wsymEditor(t)
	openWsym(t, m, "wsym")
	typeKeys(m, "l")
	m.Update(wsymMsg{seq: m.wsym.seq - 1}) // an older answer arriving late
	if !m.wsym.waiting || len(m.wsym.rows) != 2 {
		t.Fatalf("stale reply was applied: waiting=%v rows=%d", m.wsym.waiting, len(m.wsym.rows))
	}
	feed(m, "esc")
	m.Update(wsymMsg{seq: 1}) // after close: ignored, no panic
}

func TestWsymNoServerNamesTheReason(t *testing.T) {
	n := newEditor(t)
	feed(n, "enter", "down", "down", "enter", ":")
	typeKeys(n, "wsym")
	feed(n, "enter")
	if n.mode == modeWorkspaceSyms || !strings.Contains(n.active().Message(), "no language server") {
		t.Fatalf("msg = %q mode=%v", n.active().Message(), n.mode)
	}
}

func TestWsymRestrictedNamesTrust(t *testing.T) {
	m, _, _ := trustedEditor(t)
	msg, ok := m.wsymCommand("wsym")
	if !ok || !strings.Contains(msg, "restricted: trust this folder to search workspace symbols") || m.mode == modeWorkspaceSyms {
		t.Fatalf("msg = %q mode=%v", msg, m.mode)
	}
}

func TestWsymPaletteEntry(t *testing.T) {
	m, _, _ := wsymEditor(t)
	for _, c := range m.Commands() {
		if c.Title == "Go to symbol in workspace" {
			pumpCmd(m, c.Run())
			if m.mode != modeWorkspaceSyms || len(m.wsym.rows) != 2 {
				t.Fatalf("mode=%v", m.mode)
			}
			return
		}
	}
	t.Fatal("no palette entry")
}
