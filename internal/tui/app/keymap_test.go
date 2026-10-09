package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// typingSurface reports it is taking text (an insert-mode buffer).
type typingSurface struct {
	stubSurface
	typing bool
}

func (s *typingSurface) CapturesInput() bool { return s.typing }

// TestKeyRemapsTranslateOnceAndNeverWhileTyping pins F-058.
func TestKeyRemapsTranslateOnceAndNeverWhileTyping(t *testing.T) {
	t.Cleanup(func() { kit.SetKeyMap(nil) })
	kit.SetKeyMap(map[string]string{"x": "n", "ctrl+k": "ctrl+p"})
	s := &typingSurface{stubSurface: stubSurface{id: "ws", title: "WS", consumeKeys: true}}
	a := New("test", s)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	a.Update(keyPress("x"))
	if got := s.keys[len(s.keys)-1]; got != "n" {
		t.Fatalf("x reached the surface as %q, want n", got)
	}
	s.typing = true
	a.Update(keyPress("x"))
	if got := s.keys[len(s.keys)-1]; got != "x" {
		t.Fatalf("while typing, x must stay x (got %q)", got)
	}
	a.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}) // chords translate even while typing
	if a.palette == nil {
		t.Fatal("ctrl+k (remapped to ctrl+p) did not open the palette")
	}
}
