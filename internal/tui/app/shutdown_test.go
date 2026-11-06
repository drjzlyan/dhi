package app

import "testing"

type shutStub struct {
	dirtyStub
	closed int
}

func (s *shutStub) Shutdown()              { s.closed++ }
func (s *shutStub) KeepForNextTime() error { return nil }
func (s *shutStub) DiscardSwaps()          {}

// F-165: every way out of DHI calls Shutdown once, so the rebase shim's
// folder and socket never outlive the process.
func TestEveryQuitPathShutsSurfacesDown(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dirty []string
		keys  []string
	}{
		{"nothing unsaved", nil, []string{"ctrl+c"}},
		{"save all", []string{"a.go"}, []string{"ctrl+c", "s"}},
		{"keep for next time", []string{"a.go"}, []string{"ctrl+c", "k"}},
		{"quit without saving", []string{"a.go"}, []string{"ctrl+c", "d"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newDirtyApp(t)
			s := &shutStub{dirtyStub: dirtyStub{stubSurface: stubSurface{id: "ed", title: "Ed"}, names: tc.dirty}}
			a.surfaces = append(a.surfaces, s)
			var quit bool
			for _, k := range tc.keys {
				_, cmd := a.Update(keyPress(k))
				quit = isQuit(cmd)
			}
			if !quit || s.closed != 1 {
				t.Fatalf("quit=%v shutdowns=%d", quit, s.closed)
			}
		})
	}
}

func TestCancelledQuitDoesNotShutDown(t *testing.T) {
	a, _ := newDirtyApp(t)
	s := &shutStub{dirtyStub: dirtyStub{stubSurface: stubSurface{id: "ed", title: "Ed"}, names: []string{"a.go"}}}
	a.surfaces = append(a.surfaces, s)
	a.Update(keyPress("ctrl+c"))
	a.Update(keyPress("esc"))
	if s.closed != 0 {
		t.Fatal("shut down although the quit was cancelled")
	}
}
