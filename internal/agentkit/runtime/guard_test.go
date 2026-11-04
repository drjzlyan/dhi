package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// A CLI that keeps issuing the same Bash call, then would sleep: DHI
// must end it by name long before the sleep (G-C4).
const loopStub = `#!/bin/sh
i=0
while [ $i -lt 10 ]; do
  echo '{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./...","description":"x"}}]}}'
  i=$((i+1))
done
sleep 5
echo '{"type":"result","subtype":"success","is_error":false,"result":"late"}'
`

// The same tool with different inputs is progress, not a loop.
const variedStub = `#!/bin/sh
i=0
while [ $i -lt 12 ]; do
  echo '{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a/'$i'.go"}}]}}'
  i=$((i+1))
done
echo '{"type":"result","subtype":"success","is_error":false,"result":"done"}'
`

func TestRepeatedToolCallStopsTheRunByName(t *testing.T) {
	h := newHarnessScript(t, baseDoc(), loopStub)
	e, _ := h.rt.agentEntry("scout")
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	start := time.Now()
	run, _ := h.rt.cliSpawnOnce(context.Background(), e, trig, spawnInput{prompt: "p", workdir: h.ws.Root})
	if time.Since(start) > 4*time.Second {
		t.Fatal("the looping spawn was not stopped")
	}
	if run.Status != tasks.RunError || run.StopReason != "loop" {
		t.Fatalf("status %s stop %q, want error/loop", run.Status, run.StopReason)
	}
	if !strings.Contains(run.Error, "same tool call 8 times in a row") || !strings.Contains(run.Error, "go test") {
		t.Fatalf("error = %q", run.Error)
	}
}

func TestDistinctToolInputsAreNotALoop(t *testing.T) {
	h := newHarnessScript(t, baseDoc(), variedStub)
	e, _ := h.rt.agentEntry("scout")
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	run, _ := h.rt.cliSpawnOnce(context.Background(), e, trig, spawnInput{prompt: "p", workdir: h.ws.Root})
	if run.Status != tasks.RunOK || run.StopReason != "" {
		t.Fatalf("status %s stop %q (%s), want a normal run", run.Status, run.StopReason, run.Error)
	}
}

// Streamed usage past the task token cap ends the run mid-stream, and the
// partial spend is charged to the ledger.
const spendStub = `#!/bin/sh
echo '{"type":"assistant","message":{"id":"a","usage":{"input_tokens":600,"output_tokens":100},"content":[{"type":"text","text":"one"}]}}'
echo '{"type":"assistant","message":{"id":"a","usage":{"input_tokens":600,"output_tokens":100},"content":[{"type":"text","text":"same message again"}]}}'
echo '{"type":"assistant","message":{"id":"b","usage":{"input_tokens":700,"output_tokens":100},"content":[{"type":"text","text":"two"}]}}'
sleep 5
echo '{"type":"result","subtype":"success","is_error":false,"result":"late"}'
`

func TestInFlightSpendStopsAtTheCap(t *testing.T) {
	h := newHarnessScript(t, baseDoc(), spendStub)
	h.rt.cfg.Spend = budget.Open(h.ws.Root)
	e, _ := h.rt.agentEntry("scout")
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	// Message a counts once (700); b brings it to 1500 >= 1200.
	start := time.Now()
	run, _ := h.rt.cliSpawnOnce(context.Background(), e, trig, spawnInput{
		prompt: "p", workdir: h.ws.Root, taskSlug: "t1",
		budgets: budget.Limits{TaskTokens: 1200},
	})
	if time.Since(start) > 4*time.Second {
		t.Fatal("the spawn was not stopped at the cap")
	}
	if run.Status != tasks.RunError || run.StopReason != "budget" {
		t.Fatalf("status %s stop %q (%s)", run.Status, run.StopReason, run.Error)
	}
	if !strings.Contains(run.Error, "budgets.task_tokens") {
		t.Fatalf("error does not name the setting: %q", run.Error)
	}
	if run.TokensIn != 1500 {
		t.Fatalf("charged tokens = %d, want 1500 (a counted once)", run.TokensIn)
	}
}

func TestInFlightSpendBelowTheCapRunsOn(t *testing.T) {
	h := newHarnessScript(t, baseDoc(), variedStub)
	h.rt.cfg.Spend = budget.Open(h.ws.Root)
	e, _ := h.rt.agentEntry("scout")
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	run, _ := h.rt.cliSpawnOnce(context.Background(), e, trig, spawnInput{
		prompt: "p", workdir: h.ws.Root, taskSlug: "t1", budgets: budget.Limits{TaskTokens: 1200},
	})
	if run.Status != tasks.RunOK {
		t.Fatalf("status %s (%s)", run.Status, run.Error)
	}
}
