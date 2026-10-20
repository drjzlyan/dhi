package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// altTaker is a typing surface that may claim alt+1..9 (a terminal).
type altTaker struct {
	capturingSurface
	takes bool
}

func (a *altTaker) TakesAltDigits() bool { return a.takes }

func altKey(d rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: d, Mod: tea.ModAlt} }

// ADR-0041: alt+N switches views even while a surface takes text, so a
// focused buffer (which keeps the digits) is never a dead end.
func TestAltDigitSwitchesViewsWhileTyping(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ed := &altTaker{capturingSurface: capturingSurface{stubSurface: stubSurface{id: "editor", title: "Editor", consumeKeys: true}, typing: true}}
	a := New("t", ed, &stubSurface{id: "workspace", title: "Workspace"})
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	a.Update(keyPress("2"))
	if a.active != 0 {
		t.Fatal("a typing surface keeps plain digits")
	}
	a.Update(altKey('2'))
	if a.Active().Meta().ID != "workspace" {
		t.Fatalf("alt+2 must select view 2, active = %s", a.Active().Meta().ID)
	}
	a.Update(altKey('9')) // out of range: ignored
	if a.Active().Meta().ID != "workspace" {
		t.Fatal("alt+9 with two views must do nothing")
	}
}

func TestAltDigitStaysWithASurfaceThatUsesIt(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	term := &altTaker{capturingSurface: capturingSurface{stubSurface: stubSurface{id: "editor", title: "Editor", consumeKeys: true}, typing: true}, takes: true}
	a := New("t", term, &stubSurface{id: "workspace", title: "Workspace"})
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.Update(altKey('2'))
	if a.active != 0 {
		t.Fatal("a focused terminal keeps alt+2 for its own tabs")
	}
	if len(term.keys) == 0 || term.keys[len(term.keys)-1] != "alt+2" {
		t.Fatalf("alt+2 did not reach the surface: %v", term.keys)
	}
}
