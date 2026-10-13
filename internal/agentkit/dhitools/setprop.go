package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/setprop"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// SettingCheck validates a proposed change against the settings schema
// and the full layer stack, returning the current effective value (the
// app wires settings.CheckChange; dhitools cannot import settings).
type SettingCheck func(key, value, layer string) (old string, err error)

// settingTools serves settings_propose (F-070): the agent proposes, the
// human applies. A harness tool, so every agent can ask.
func (d Deps) settingTools() []tool {
	if d.SetProps == nil || d.SettingCheck == nil {
		return nil
	}
	return []tool{{
		info: mcp.ToolInfo{
			Name: "settings_propose",
			Description: "Propose a DHI settings change when a setting is in your way (a budget too tight for the task, a scope that " +
				"keeps asking, max_iterations too low…). Nothing changes until the human approves it. " +
				"{\"key\":\"budgets.task_usd\",\"value\":\"5\",\"rationale\":\"this migration touches 40 files\"}. " +
				"layer is \"workspace\" (default) or \"user\".",
			InputSchema: json.RawMessage(`{"type":"object","required":["key","value","rationale"],"properties":{"key":{"type":"string"},"value":{"type":"string"},"layer":{"type":"string","enum":["workspace","user"]},"rationale":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Key       string `json:"key"`
				Value     string `json:"value"`
				Layer     string `json:"layer"`
				Rationale string `json:"rationale"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Rationale) == "" {
				return nil, fmt.Errorf("a rationale is required: say why the human should change it")
			}
			if a.Layer == "" {
				a.Layer = setprop.LayerWorkspace
			}
			old, err := d.SettingCheck(strings.TrimSpace(a.Key), a.Value, a.Layer)
			if err != nil {
				return nil, err
			}
			return setprop.Proposal{Key: strings.TrimSpace(a.Key), Value: strings.TrimSpace(a.Value), Old: old,
				Layer: a.Layer, Rationale: strings.TrimSpace(a.Rationale)}, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			p := dec.(setprop.Proposal)
			p.Agent = d.Agent.ID
			p, err := d.SetProps.Propose(p)
			if err != nil {
				return "", err
			}
			if d.Bus != nil && d.Channel != "" {
				_, _ = d.Bus.Post(bus.Message{
					Channel: d.Channel, Thread: d.Thread, Author: d.Agent.ID,
					Text: fmt.Sprintf("@%s I propose `%s`: %s → %s (%s layer) — %s. Review it in Settings → PROPOSALS",
						bus.Human, p.Key, orNone(p.Old), p.Value, p.Layer, p.Rationale),
				})
			}
			return fmt.Sprintf("proposed %s = %s; it applies only if the human approves", p.Key, p.Value), nil
		},
	}}
}

// projectTools serves project_update (F-080): an agent that learned how
// the user works proposes a change to one field of the project brief;
// the human applies it from Settings → PROPOSALS. A harness tool.
func (d Deps) projectTools() []tool {
	if d.SetProps == nil || d.WS == nil {
		return nil
	}
	fields := make([]string, len(brief.Fields))
	for i, f := range brief.Fields {
		fields[i] = f.Key
	}
	enum, _ := json.Marshal(fields)
	return []tool{{
		info: mcp.ToolInfo{
			Name: "project_update",
			Description: "Propose a change to the project brief every employee reads (what the project is and how the user works). " +
				"Send the field's WHOLE new text: {\"field\":\"process\",\"text\":\"Trunk-based; PRs under 300 lines\",\"rationale\":\"you said so in the interview\"}. " +
				"Nothing changes until the human approves it.",
			InputSchema: json.RawMessage(`{"type":"object","required":["field","text","rationale"],"properties":{"field":{"type":"string","enum":` +
				string(enum) + `},"text":{"type":"string"},"rationale":{"type":"string"}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a struct {
				Field     string `json:"field"`
				Text      string `json:"text"`
				Rationale string `json:"rationale"`
			}
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Rationale) == "" {
				return nil, fmt.Errorf("a rationale is required: say where this came from")
			}
			cur, _, err := brief.Load(d.WS.Root)
			if err != nil {
				return nil, err
			}
			var probe brief.Brief
			if err := probe.Set(a.Field, a.Text); err != nil {
				return nil, err
			}
			return setprop.Proposal{Key: "project." + a.Field, Value: strings.TrimSpace(a.Text), Old: cur.Get(a.Field),
				Layer: setprop.LayerProject, Rationale: strings.TrimSpace(a.Rationale)}, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			p := dec.(setprop.Proposal)
			p.Agent = d.Agent.ID
			p, err := d.SetProps.Propose(p)
			if err != nil {
				return "", err
			}
			if d.Bus != nil && d.Channel != "" {
				_, _ = d.Bus.Post(bus.Message{
					Channel: d.Channel, Thread: d.Thread, Author: d.Agent.ID,
					Text: fmt.Sprintf("@%s I propose updating the project brief's %s — %s. Review it in Settings → PROPOSALS",
						bus.Human, strings.TrimPrefix(p.Key, "project."), p.Rationale),
				})
			}
			return fmt.Sprintf("proposed %s; it applies only if the human approves", p.Key), nil
		},
	}}
}
