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
var harnessSlugs = []string{"plan_update", "lesson_record", "settings_propose", "project_update", "work_branch", "permission_request"}

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

// lessonTools lets an agent record a durable lesson while it works
// (F-068): private by default, "user" for the human's stated preference.
func (d Deps) lessonTools() []tool {
	if d.LessonRecord == nil {
		return nil
	}
	return []tool{{
		info: mcp.ToolInfo{
			Name: "lesson_record",
			Description: "Remember a durable lesson for future work: ONE short imperative rule specific to this codebase, " +
				"its tools or the human (not generic advice). {\"text\":\"Run go test with -race in this repo\",\"tags\":[\"go\",\"tests\"]}. " +
				"Use \"scope\":\"user\" only when the human states a personal preference; it then applies to every agent.",
			InputSchema: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"scope":{"type":"string","enum":["private","user"]}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Text  string   `json:"text"`
				Tags  []string `json:"tags"`
				Scope string   `json:"scope"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			a.Text = strings.TrimSpace(a.Text)
			if a.Text == "" || len([]rune(a.Text)) > 300 {
				return nil, fmt.Errorf("text must be 1..300 characters")
			}
			if a.Scope == "" {
				a.Scope = "private"
			}
			if a.Scope != "private" && a.Scope != "user" {
				return nil, fmt.Errorf("scope must be private or user")
			}
			return a, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			b, _ := json.Marshal(dec)
			var a struct {
				Text  string   `json:"text"`
				Tags  []string `json:"tags"`
				Scope string   `json:"scope"`
			}
			_ = json.Unmarshal(b, &a)
			return d.LessonRecord(a.Text, a.Scope, a.Tags)
		},
	}}
}
