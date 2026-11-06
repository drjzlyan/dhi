package app

import "testing"

type sessionStub struct {
	dirtyStub
	sessions int
}

func (s *sessionStub) SaveSession() { s.sessions++ }

// F-149: every quit path asks surfaces to save their session.
func TestQuitSavesSession(t *testing.T) {
	for _, c := range []struct {
		name  string
		dirty []string
		keys  []string
	}{
		{"clean", nil, []string{"ctrl+c"}},
		{"save and quit", []string{"a.go"}, []string{"ctrl+c", "s"}},
		{"discard and quit", []string{"a.go"}, []string{"ctrl+c", "d"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := &sessionStub{dirtyStub: dirtyStub{stubSurface: stubSurface{id: "editor", title: "Editor"}, names: c.dirty}}
			a := New("test", d)
			var quit bool
			for _, k := range c.keys {
				_, cmd := a.Update(keyPress(k))
				quit = quit || isQuit(cmd)
			}
			if !quit || d.sessions != 1 {
				t.Fatalf("quit=%v sessions=%d", quit, d.sessions)
			}
		})
	}
	// Cancelling the guard saves nothing.
	d := &sessionStub{dirtyStub: dirtyStub{stubSurface: stubSurface{id: "editor", title: "Editor"}, names: []string{"a.go"}}}
	a := New("test", d)
	a.Update(keyPress("ctrl+c"))
	a.Update(keyPress("esc"))
	if d.sessions != 0 {
		t.Fatal("session saved without quitting")
	}
}
