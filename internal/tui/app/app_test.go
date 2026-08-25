package app

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/placeholder"
	"github.com/drjzlyan/dhi/internal/tui/surfaces/workspace"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// stubSurface records every interaction for assertions.
type stubSurface struct {
	id, title   string
	keys        []string
	resized     int
	updates     int
	consumeKeys bool
}

func (s *stubSurface) Meta() surfaces.Meta { return surfaces.Meta{ID: s.id, Title: s.title} }
func (s *stubSurface) Init() tea.Cmd       { return nil }
func (s *stubSurface) Resize(w, h int)     { s.resized++; _, _ = w, h }
func (s *stubSurface) Update(tea.Msg) tea.Cmd {
	s.updates++
	return nil
}
func (s *stubSurface) HandleKey(k string) bool {
	s.keys = append(s.keys, k)
	return s.consumeKeys
}

func newTestApp(t *testing.T) (*App, []*stubSurface) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	stubs := []*stubSurface{
		{id: "home", title: "Home"},
		{id: "editor", title: "Editor", consumeKeys: true},
		{id: "trees", title: "Trees"},
	}
	a := New("test", stubs[0], stubs[1], stubs[2])
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return a, stubs
}

func TestNumberKeySwitchesSurface(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(keyPress("3"))
	if a.active != 2 {
		t.Fatalf("active=%d want 2", a.active)
	}
	a.Update(keyPress("9")) // out of range → ignored
	if a.active != 2 {
		t.Fatalf("out-of-range key changed active to %d", a.active)
	}
}

func TestTabCyclesWithWraparound(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(keyPress("tab"))
	a.Update(keyPress("tab"))
	a.Update(keyPress("tab"))
	if a.active != 0 {
		t.Fatalf("wrap failed, active=%d", a.active)
	}
	a.Update(keyPress("shift+tab"))
	if a.active != 2 {
		t.Fatalf("shift+tab wrap failed, active=%d", a.active)
	}
}

func TestUnknownKeysForwardToActiveSurface(t *testing.T) {
	a, st := newTestApp(t)
	a.Update(keyPress("j")) // home doesn't consume; still forwarded
	if !slices.Contains(st[0].keys, "j") {
		t.Fatal("key not forwarded to active surface")
	}
}

func TestHelpTogglesAndQuitReturnsCmd(t *testing.T) {
	a, _ := newTestApp(t)
	a.Update(keyPress("?"))
	if !a.showHelp {
		t.Fatal("? did not open help")
	}
	a.Update(keyPress("?"))
	if a.showHelp {
		t.Fatal("? did not close help")
	}
	if _, ok := a.handleGlobal("ctrl+c"); !ok {
		t.Fatal("ctrl+c must be handled globally")
	}
	if !a.quitting {
		t.Fatal("quit state not set")
	}
}

func TestResizeBroadcastsToAllSurfaces(t *testing.T) {
	a, st := newTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i, s := range st {
		if s.resized < 1 {
			t.Fatalf("surface %d never resized", i)
		}
	}
}

func TestViewCompositionGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())

	a := New("0.1.0",
		workspace.New("0.1.0", nil, nil, nil),
		placeholder.New("editor", "Editor", "M2", "Files · buffers · terminal · git · chat."),
		placeholder.New("settings", "Settings", "M2+", "Everything configurable."))
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	golden.Snapshot(t, "shell_workspace_100x30", a.compose())

	a.Update(keyPress("2")) // editor placeholder
	golden.Snapshot(t, "shell_editor_100x30", a.compose())

	a.Update(keyPress("1"))
	a.Update(keyPress("?")) // help overlay
	golden.Snapshot(t, "shell_help_workspace_100x30", a.compose())
}

// stubGate satisfies Gate and records routing.
type stubGate struct {
	resized  int
	updates  int
	finished bool
}

func (g *stubGate) Init() tea.Cmd          { return nil }
func (g *stubGate) Resize(w, h int)        { g.resized++ }
func (g *stubGate) Update(tea.Msg) tea.Cmd { g.updates++; return nil }
func (g *stubGate) View() string           { return "GATE" }
func (g *stubGate) Finished() bool         { return g.finished }

func TestGateOwnsBodyUntilFinished(t *testing.T) {
	a, st := newTestApp(t)
	gate := &stubGate{}
	a.SetGate(gate)
	a.Init()

	a.Update(keyPress("2")) // swallowed: surfaces must not see keys
	if a.active != 0 {
		t.Fatalf("key press during gate changed surface to %d", a.active)
	}
	if len(st[1].keys) != 0 {
		t.Fatalf("inactive surfaces received keys during gate: %v", st[1].keys)
	}
	if body := a.compose(); !strings.Contains(body, "GATE") || strings.Contains(body, "stub:") {
		t.Fatalf("gate does not own the body:\n%s", body)
	}

	msg := testMsg{}
	a.Update(msg)
	if gate.updates != 1 {
		t.Fatal("non-key message not routed to gate")
	}

	gate.finished = true
	a.Update(testMsg{})
	if body := a.compose(); !strings.Contains(body, "stub:home") {
		t.Fatalf("shell did not resume after finish:\n%s", body)
	}

	a.Update(keyPress("3"))
	if a.active != 2 {
		t.Fatal("keys dead after gate finished")
	}
}

func TestGateResizedWithShell(t *testing.T) {
	a, _ := newTestApp(t)
	gate := &stubGate{}
	a.SetGate(gate)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if gate.resized == 0 {
		t.Fatal("gate never resized")
	}
}

type testMsg struct{}

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		r := []rune(s)
		return tea.KeyPressMsg{Text: s, Code: r[0]}
	}
}

func (s *stubSurface) View() string { return "stub:" + s.id }
