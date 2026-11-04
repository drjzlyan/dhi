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
