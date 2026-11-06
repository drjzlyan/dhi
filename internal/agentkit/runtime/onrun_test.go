package runtime

import (
	"context"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-164: a finished run reaches the OnRun hook (trace export) once.
func TestOnRunHookSeesFinishedRun(t *testing.T) {
	h := newHarnessMulti(t, baseDoc(), map[string]string{"claude": stdinStub})
	var got []tasks.Run
	h.rt.cfg.OnRun = func(r tasks.Run, _ int) { got = append(got, r) }
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	h.rt.Handle(context.Background(), trig)
	h.rt.bg.Wait()
	if len(got) != 1 || got[0].Agent != "scout" || got[0].Runtime != "cli:claude" {
		t.Fatalf("OnRun runs = %+v", got)
	}
}

// F-164: read-only turns and /compact spawns reach OnRun too, not only
// the write-turn loop.
func TestOnRunHookSeesReadOnlyAndCompactSpawns(t *testing.T) {
	h := episodeHarness(t)
	var got []tasks.Run
	h.rt.cfg.OnRun = func(r tasks.Run, _ int) { got = append(got, r) }
	trig := dmTrigger(t, h, "/ask where is main")
	if err := h.rt.ReadOnlyTurn(context.Background(), "scout", trig, "ask", "where is main"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Agent != "scout" {
		t.Fatalf("read-only turn: OnRun runs = %+v", got)
	}
	root := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout remember kiwi"})
	if err := h.rt.Turn(context.Background(), "scout", root); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("write turn: OnRun runs = %d", len(got))
	}
	if err := h.rt.Compact(context.Background(), "scout", "#general", root.ID); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("compaction must reach OnRun: runs = %d", len(got))
	}
}
