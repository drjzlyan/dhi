package runtime

import (
	"context"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
)

// F-115: views hold the runtime through interfaces; a workspace without a
// roster has none, and calls on it must be harmless.
func TestNilRuntimeIsHarmless(t *testing.T) {
	var r *Runtime
	r.Handle(context.Background(), bus.Message{Channel: "general", Author: bus.Human, Text: "hi"})
	if ids := r.AgentIDs(); len(ids) != 0 {
		t.Fatal(ids)
	}
	if _, ok := r.Manifest("x"); ok {
		t.Fatal("manifest on nil")
	}
	if r.Working("general", 0) || r.AgentWorking("x") {
		t.Fatal("nil runtime working")
	}
}

// F-117: the chat's stop key cancels every turn on the channel, and Busy
// counts what runs and what waits.
func TestCancelChannelAndBusy(t *testing.T) {
	r := &Runtime{}
	k1, k2, other := workKey("#general", 1), workKey("#general", 2), workKey("dm:x", 3)
	var ctxs []context.Context
	for _, k := range []string{k1, k2, other} {
		ctx, _ := r.cancellable(context.Background(), k)
		r.workStart(k)
		ctxs = append(ctxs, ctx)
	}
	r.waitStart("#general")
	if run, q := r.Busy("#general"); run != 2 || q != 1 {
		t.Fatalf("busy = %d running, %d queued", run, q)
	}
	if n := r.CancelChannel("#general"); n != 2 {
		t.Fatalf("cancelled %d, want 2", n)
	}
	if ctxs[0].Err() == nil || ctxs[1].Err() == nil || ctxs[2].Err() != nil {
		t.Fatal("cancel reached the wrong turns")
	}
	if n := r.CancelChannel("#nothing"); n != 0 {
		t.Fatal(n)
	}
}
