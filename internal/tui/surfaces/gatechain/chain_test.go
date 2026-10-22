package gatechain

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
)

type fake struct {
	name     string
	inited   int
	done     bool
	finishOn string // key that finishes it
	queued   tea.Cmd
	taken    int
	resized  [2]int
}

func (f *fake) Init() tea.Cmd          { f.inited++; return nil }
func (f *fake) Resize(w, h int)        { f.resized = [2]int{w, h} }
func (f *fake) Update(tea.Msg) tea.Cmd { return nil }
func (f *fake) View() string           { return f.name }
func (f *fake) Finished() bool         { return f.done }
func (f *fake) HandleKey(k string) bool {
	if k == f.finishOn {
		f.done = true
	}
	f.queued = func() tea.Msg { return k }
	return true
}
func (f *fake) TakeCmd() tea.Cmd { c := f.queued; f.queued = nil; f.taken++; return c }

func TestSecondGateStartsOnlyAfterFirstFinishes(t *testing.T) {
	a := &fake{name: "bootstrap", finishOn: "enter"}
	b := &fake{name: "wizard"}
	c := New(a, nil, b)
	c.Resize(80, 24)
	c.Init()
	if a.inited != 1 || b.inited != 0 || c.View() != "bootstrap" {
		t.Fatalf("a=%d b=%d view=%q", a.inited, b.inited, c.View())
	}
	c.HandleKey("x")
	if b.inited != 0 || c.Finished() {
		t.Fatal("second gate started early")
	}
	c.HandleKey("enter")
	if b.inited != 1 || c.View() != "wizard" || c.Finished() {
		t.Fatalf("b.inited=%d view=%q", b.inited, c.View())
	}
	if b.resized != [2]int{80, 24} {
		t.Fatalf("late gate not sized: %v", b.resized)
	}
	if c.TakeCmd() == nil {
		t.Fatal("queued command from the first gate was lost")
	}
	b.done = true
	c.Update(nil)
	if !c.Finished() || c.View() != "" {
		t.Fatal("chain should finish after the last gate")
	}
}

func TestAlreadyFinishedGatesAreSkipped(t *testing.T) {
	a := &fake{name: "a", done: true}
	b := &fake{name: "b"}
	c := New(a, b)
	c.Init()
	if a.inited != 0 || b.inited != 1 || c.View() != "b" {
		t.Fatalf("a=%d b=%d view=%q", a.inited, b.inited, c.View())
	}
}

func TestEmptyChainIsFinished(t *testing.T) {
	c := New(nil)
	if !c.Finished() || c.Init() != nil || c.HandleKey("x") || strings.TrimSpace(c.View()) != "" {
		t.Fatal("empty chain must be inert and finished")
	}
}

type relaunching struct{ fake }

func (r *relaunching) NeedsRelaunch() bool { return true }

func TestNeedsRelaunchIsForwarded(t *testing.T) {
	if !New(&fake{}, &relaunching{}).NeedsRelaunch() || New(&fake{}).NeedsRelaunch() {
		t.Fatal("relaunch flag not forwarded correctly")
	}
}
