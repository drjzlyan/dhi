package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

type queuedMsg struct{}

// queueingSurface queues a command when it handles a key, like the editor
// starting a streaming search.
type queueingSurface struct {
	stubSurface
	queued tea.Cmd
}

func (s *queueingSurface) HandleKey(k string) bool {
	s.queued = func() tea.Msg { return queuedMsg{} }
	return true
}

func (s *queueingSurface) TakeCmd() tea.Cmd {
	c := s.queued
	s.queued = nil
	return c
}

// TestShellDrainsSurfaceCmds pins the CmdSource seam: work a key starts
// reaches the runtime (the editor's search used to hang on "searching…").
func TestShellDrainsSurfaceCmds(t *testing.T) {
	s := &queueingSurface{stubSurface: stubSurface{id: "editor", title: "Editor"}}
	a := New("test", s)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := a.Update(keyPress("s"))
	if cmd == nil {
		t.Fatal("the queued command was dropped")
	}
	if _, ok := cmd().(queuedMsg); !ok {
		t.Fatal("wrong command returned")
	}
	if s.queued != nil {
		t.Fatal("TakeCmd must drain the queue")
	}
}
