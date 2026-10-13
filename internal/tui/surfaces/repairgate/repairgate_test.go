package repairgate

import (
	"errors"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/repair"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestNothingToSayIsNotNeeded(t *testing.T) {
	if New("1", repair.Result{}, nil).Needed() {
		t.Fatal("empty gate needed")
	}
}

func TestAskThenFix(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	ran := 0
	ask := []repair.Repair{
		{ID: "a", What: "team core: its lead ada is gone: you lead it", Apply: func() error { ran++; return nil }},
		{ID: "b", What: "bob uses persona x, which no longer exists: drop it", Apply: func() error { return errors.New("read-only") }},
	}
	g := New("9.9.9", repair.Result{Done: []string{"keep DHI's runtime files out of git: write .dhi/.gitignore"}}, ask)
	g.Resize(100, 30)
	if !g.Needed() {
		t.Fatal("not needed")
	}
	v := ansi.Strip(g.View())
	for _, want := range []string{"Fixed on its own", ".gitignore", "with your OK", "you lead it", "enter fix them"} {
		if !strings.Contains(v, want) {
			t.Fatalf("lacks %q:\n%s", want, v)
		}
	}
	golden.Snapshot(t, "repairgate_ask", v)
	g.HandleKey("enter")
	if ran != 1 || g.Finished() {
		t.Fatalf("ran %d finished %v", ran, g.Finished())
	}
	v = ansi.Strip(g.View())
	if !strings.Contains(v, "Could not fix") || !strings.Contains(v, "read-only") {
		t.Fatalf("result:\n%s", v)
	}
	g.HandleKey("enter")
	if !g.Finished() {
		t.Fatal("enter did not continue")
	}
}

func TestNotNowLeavesItAlone(t *testing.T) {
	ran := false
	g := New("1", repair.Result{}, []repair.Repair{{What: "x", Apply: func() error { ran = true; return nil }}})
	g.HandleKey("esc")
	if ran || !g.Finished() {
		t.Fatalf("ran %v finished %v", ran, g.Finished())
	}
}
