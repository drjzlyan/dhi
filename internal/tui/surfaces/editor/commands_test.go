package editor

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/tui/surfaces"
)

var _ surfaces.CommandProvider = (*Model)(nil)

func titles(m *Model) []string {
	var out []string
	for _, c := range m.Commands() {
		out = append(out, c.Title)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestPaletteCommandsReflectEditorState(t *testing.T) {
	m := newEditor(t)
	base := titles(m)
	for _, want := range []string{"Find file", "Toggle terminal", "Toggle git panel"} {
		if !has(base, want) {
			t.Errorf("tree state missing %q: %v", want, base)
		}
	}
	for _, absent := range []string{"Format document", "Run tests in this package", "Start debugging"} {
		if has(base, absent) {
			t.Errorf("%q needs a buffer but is offered with none open", absent)
		}
	}

	feed(m, "enter", "down", "down", "enter") // open a buffer
	withBuf := titles(m)
	for _, want := range []string{"Save file", "Format document", "Go to symbol", "Run tests in this package", "Toggle breakpoint on this line", "Start debugging"} {
		if !has(withBuf, want) {
			t.Errorf("buffer state missing %q: %v", want, withBuf)
		}
	}
	if has(withBuf, "Stop debugging") || has(withBuf, "Review agent suggestions") {
		t.Errorf("session/proposal commands must be hidden when inactive: %v", withBuf)
	}
}

func TestPaletteCommandsRunThroughTheSameExPath(t *testing.T) {
	m := newEditor(t)
	feed(m, "enter", "down", "down", "enter")
	var run func()
	for _, c := range m.Commands() {
		if c.Title == "Toggle breakpoint on this line" {
			run = func() { c.Run() }
		}
	}
	run()
	if got := m.active().Message(); !strings.Contains(got, "breakpoint set") {
		t.Fatalf("message = %q", got)
	}
	if len(m.breakpoints) != 1 {
		t.Fatalf("breakpoints = %v", m.breakpoints)
	}
}

func TestPaletteOffersPairingAndProposals(t *testing.T) {
	h, _ := pairChatEditor(t)
	if !has(titles(h.m), "Pair with @scout") {
		t.Fatalf("roster agents should be offered: %v", titles(h.m))
	}
	typeKeys(h.m, ":pair scout")
	h.m.HandleKey("enter")
	got := titles(h.m)
	if !has(got, "End pairing with @scout") || has(got, "Pair with @scout") {
		t.Fatalf("pairing state not reflected: %v", got)
	}
	if err := h.m.Propose(h.m.active().Path(), "func f", "func g", "", "scout"); err != nil {
		t.Fatal(err)
	}
	if !has(titles(h.m), "Review agent suggestions") {
		t.Fatal("pending proposals should add a review command")
	}
}
