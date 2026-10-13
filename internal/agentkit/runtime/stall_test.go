package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
)

const quietStub = `#!/bin/sh
sleep 1
echo '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"S1","usage":{"input_tokens":1,"output_tokens":1}}'
`

// M32: a spawn silent because the agent waits on the human (an approval,
// a secret) is not stalled; without a pending ask, the watchdog fires.
func TestStallWatchdogWaitsWhileTheAgentWaitsOnYou(t *testing.T) {
	h := newHarnessScript(t, baseDoc(), quietStub)
	e, _ := h.rt.agentEntry("scout")
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	spawn := func() tasks.Run {
		run, _ := h.rt.cliSpawnOnce(context.Background(), e, trig, spawnInput{prompt: "p", workdir: h.ws.Root, stall: 200 * time.Millisecond})
		return run
	}
	if run := spawn(); run.Status != tasks.RunError {
		t.Fatalf("no pending ask: status %s, want the stall", run.Status)
	}
	done := make(chan struct{})
	go func() {
		_ = h.approvals.AskScope(context.Background(), "scout", "write:/x", sandbox.OpWrite, "/x", "")
		close(done)
	}()
	for len(h.approvals.List()) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if run := spawn(); run.Status != tasks.RunOK {
		t.Fatalf("pending ask: status %s (%s), want ok", run.Status, run.Error)
	}
	h.approvals.Resolve(h.approvals.List()[0].ID, false)
	<-done
}
