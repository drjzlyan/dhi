package workspace

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tutorial"
)

func TestCreatingATaskReportsItOnlyOnSuccess(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard
	var got []string
	m.SetEmitter(func(ev string) { got = append(got, ev) })

	m.HandleKey("n")
	typeInto(t, m, 0, "fix-login")
	typeInto(t, m, 1, "") // no title: the store refuses
	m.HandleKey("enter")
	if len(got) != 0 {
		t.Fatalf("a refused task reported %v", got)
	}

	typeInto(t, m, 1, "Fix login race")
	m.HandleKey("enter")
	if len(got) != 1 || got[0] != tutorial.EvTaskCreated {
		t.Fatalf("a created task reported %v", got)
	}
}
