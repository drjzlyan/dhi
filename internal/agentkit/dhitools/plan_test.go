package dhitools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
)

// F-067: plan_update is served without an allowlist entry, validates
// steps strictly, and lands on the seam without an approval.
func TestPlanUpdate(t *testing.T) {
	var got []PlanStep
	d := Deps{Agent: &manifest.Agent{ID: "dev"}, PlanUpdate: func(s []PlanStep) error { got = s; return nil }}
	h := d.Handler()
	found := false
	for _, ti := range h.Tools() {
		found = found || ti.Name == "plan_update"
	}
	if !found {
		t.Fatal("plan_update must be served without an allowlist entry")
	}
	out, isErr, err := h.CallTool(context.Background(), "plan_update",
		json.RawMessage(`{"steps":[{"title":"Write test","status":"done"},{"title":"Fix it","status":"doing"}]}`))
	if err != nil || isErr || out != "plan updated (1/2 done)" || len(got) != 2 {
		t.Fatalf("call = %q %v %v; got %+v", out, isErr, err, got)
	}
	out, isErr, _ = h.CallTool(context.Background(), "plan_update",
		json.RawMessage(`{"steps":[{"title":"x","status":"maybe"}]}`))
	if !isErr || !strings.Contains(out, "todo|doing|done|blocked") {
		t.Fatalf("bad status must refuse by name: %q", out)
	}
	for _, ti := range (Deps{Agent: &manifest.Agent{ID: "dev"}}).Handler().Tools() {
		if ti.Name == "plan_update" {
			t.Fatal("no seam, no tool")
		}
	}
}
