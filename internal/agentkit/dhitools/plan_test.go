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

// F-068: lesson_record is a harness tool with strict args.
func TestLessonRecord(t *testing.T) {
	var got []string
	d := Deps{Agent: &manifest.Agent{ID: "dev"}, LessonRecord: func(text, scope string, tags []string) (string, error) {
		got = append(got, scope+":"+text+":"+strings.Join(tags, ","))
		return "remembered", nil
	}}
	h := d.Handler()
	out, isErr, err := h.CallTool(context.Background(), "lesson_record", json.RawMessage(`{"text":"Run go test with -race","tags":["go"]}`))
	if err != nil || isErr || out != "remembered" || len(got) != 1 || got[0] != "private:Run go test with -race:go" {
		t.Fatalf("call = %q %v %v %v", out, isErr, err, got)
	}
	if out, isErr, _ := h.CallTool(context.Background(), "lesson_record", json.RawMessage(`{"text":"x","scope":"team"}`)); !isErr || !strings.Contains(out, "scope") {
		t.Fatalf("bad scope must refuse: %q", out)
	}
}
