package settings

import "testing"

// F-149: restore session is a Settings toggle, on by default.
func TestRestoreSessionToggle(t *testing.T) {
	m, _ := newSurface(t)
	if !m.cfg.Editor.RestoreSession {
		t.Fatal("restore session must default on")
	}
	m.cursor = rowRestoreSession
	feed(m, "enter")
	if m.cfg.Editor.RestoreSession {
		t.Fatal("toggle no-op")
	}
}
