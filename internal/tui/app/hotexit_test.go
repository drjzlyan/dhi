package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

type hotStub struct {
	dirtyStub
	kept, discarded int
	keepErr         error
}

func (h *hotStub) KeepForNextTime() error { h.kept++; return h.keepErr }
func (h *hotStub) DiscardSwaps()          { h.discarded++ }

func newHotApp(t *testing.T) (*App, *hotStub) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	h := &hotStub{dirtyStub: dirtyStub{stubSurface: stubSurface{id: "editor", title: "Editor"}, names: []string{"a.go", "b.go"}}}
	a := New("test", h, &stubSurface{id: "other", title: "Other"})
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return a, h
}

// F-161: k keeps the edits as swaps and quits without saving or asking per buffer.
func TestQuitKeepForNextTime(t *testing.T) {
	a, h := newHotApp(t)
	a.requestQuit()
	if !strings.Contains(ansi.Strip(a.View().Content), "keep for next time") {
		t.Fatal("guard should offer keep for next time")
	}
	_, cmd := a.Update(keyPress("k"))
	if !isQuit(cmd) || h.kept != 1 || h.saved != 0 || h.discarded != 0 {
		t.Fatalf("quit=%v kept=%d saved=%d discarded=%d", isQuit(cmd), h.kept, h.saved, h.discarded)
	}
}

func TestQuitKeepFailureStays(t *testing.T) {
	a, h := newHotApp(t)
	h.keepErr = errors.New("disk full")
	a.requestQuit()
	if _, cmd := a.Update(keyPress("k")); isQuit(cmd) || a.quitAsk == nil || a.quitErr == "" {
		t.Fatal("a failed keep must not quit")
	}
}

func TestQuitWithoutSavingDiscardsSwaps(t *testing.T) {
	a, h := newHotApp(t)
	a.requestQuit()
	if _, cmd := a.Update(keyPress("d")); !isQuit(cmd) || h.discarded != 1 {
		t.Fatal("d must quit and drop swaps")
	}
}

func TestQuitKeepNotOfferedWithoutHotExit(t *testing.T) {
	a, _ := newDirtyApp(t, "a.go")
	a.requestQuit()
	if strings.Contains(ansi.Strip(a.View().Content), "keep for next time") {
		t.Fatal("offered keep with no surface able to")
	}
	if _, cmd := a.Update(keyPress("k")); isQuit(cmd) {
		t.Fatal("k quit without a hot-exit surface")
	}
}
