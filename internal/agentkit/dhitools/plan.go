package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// PlanStep is one step of an agent's published plan (F-067).
type PlanStep = episode.PlanStep

// planStatuses are the accepted step states.
var planStatuses = map[string]bool{"todo": true, "doing": true, "done": true, "blocked": true}

// harnessSlugs are served to every agent whose turn serves tools, with
// no allowlist entry: they report the agent's own work to DHI (no
// effect outside the episode), so withholding them only hides progress.
var harnessSlugs = []string{"plan_update"}

// planTools publishes the agent's plan onto its episode, where the board
// shows it live. Absent when the runtime wires no seam.
func (d Deps) planTools() []tool {
	if d.PlanUpdate == nil {
		return nil
	}
	return []tool{{
		info: mcp.ToolInfo{
			Name: "plan_update",
			Description: "Publish your plan for this task so the human can follow it. Send the WHOLE plan each time " +
				"(it replaces the last one): {\"steps\":[{\"title\":\"Write failing test\",\"status\":\"done\"}," +
				"{\"title\":\"Fix rounding\",\"status\":\"doing\"}]}. status: todo | doing | done | blocked. " +
				"Update it when a step starts or finishes; keep titles short.",
			InputSchema: json.RawMessage(`{"type":"object","required":["steps"],"properties":{"steps":{"type":"array","items":{"type":"object","required":["title","status"],"properties":{"title":{"type":"string"},"status":{"type":"string","enum":["todo","doing","done","blocked"]}},"additionalProperties":false}}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Steps []PlanStep `json:"steps"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			if len(a.Steps) == 0 || len(a.Steps) > 30 {
				return nil, fmt.Errorf("steps: send 1..30 steps (got %d)", len(a.Steps))
			}
			for i, s := range a.Steps {
				a.Steps[i].Title = strings.TrimSpace(s.Title)
				if a.Steps[i].Title == "" || !planStatuses[s.Status] {
					return nil, fmt.Errorf("step %d: a title and a status of todo|doing|done|blocked are required", i+1)
				}
			}
			return a.Steps, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			steps := dec.([]PlanStep)
			if err := d.PlanUpdate(steps); err != nil {
				return "", err
			}
			done := 0
			for _, s := range steps {
				if s.Status == "done" {
					done++
				}
			}
			return fmt.Sprintf("plan updated (%d/%d done)", done, len(steps)), nil
		},
	}}
}
