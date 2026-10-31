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
