package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// SkillHit is one DHI registry entry an agent can suggest (F-091).
type SkillHit struct {
	Name, Version, Description string
}

// skillTools serves skill_search and skill_suggest (F-091): an employee
// that would do better with a skill finds one in the DHI registry and
// suggests it; DHI verifies it and the human decides.
func (d Deps) skillTools() []tool {
	if d.SkillSearch == nil || d.SkillSuggest == nil {
		return nil
	}
	type searchArgs struct {
		Query string `json:"query"`
	}
	type suggestArgs struct {
		Entry  string `json:"entry"`
		Reason string `json:"reason"`
	}
	return []tool{
		{
			info: mcp.ToolInfo{
				Name:        "skill_search",
				Description: "Search the DHI skill registry (the only source DHI installs skills from). Args: {\"query\":\"terraform\"} (empty lists all). Returns entry names to pass to skill_suggest.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a searchArgs
				return a, args(raw, &a)
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				hits, err := d.SkillSearch(strings.TrimSpace(dec.(searchArgs).Query))
				if err != nil {
					return "", err
				}
				if len(hits) == 0 {
					return "no registry entries match", nil
				}
				var b strings.Builder
				for _, h := range hits {
					fmt.Fprintf(&b, "%s %s — %s\n", h.Name, h.Version, h.Description)
				}
				return b.String(), nil
			},
		},
		{
			info: mcp.ToolInfo{
				Name: "skill_suggest",
				Description: "Suggest installing a skill from the DHI registry when it would make you better at this kind of task. " +
					"Args: {\"entry\":\"name from skill_search\",\"reason\":\"what it would help with\"}. DHI verifies it (digest, signature) " +
					"and the human reviews every skill and script before installing it for this workspace or every workspace. Carry on without it meanwhile.",
				InputSchema: json.RawMessage(`{"type":"object","required":["entry","reason"],"properties":{"entry":{"type":"string"},"reason":{"type":"string"}},"additionalProperties":false}`),
			},
			parse: func(raw json.RawMessage) (any, error) {
				var a suggestArgs
				if err := args(raw, &a); err != nil {
					return nil, err
				}
				a.Entry, a.Reason = strings.TrimSpace(a.Entry), strings.TrimSpace(a.Reason)
				if a.Entry == "" || a.Reason == "" {
					return nil, fmt.Errorf("entry and reason are required")
				}
				return a, nil
			},
			exec: func(ctx context.Context, dec any) (string, error) {
				a := dec.(suggestArgs)
				what, err := d.SkillSuggest(ctx, a.Entry, a.Reason)
				if err != nil {
					return "", err
				}
				if d.Bus != nil && d.Channel != "" {
					_, _ = d.Bus.Post(bus.Message{
						Channel: d.Channel, Thread: d.Thread, Author: d.Agent.ID,
						Text: fmt.Sprintf("@%s I suggest installing %s (%s) — %s. Review it in Settings → PROPOSALS",
							bus.Human, a.Entry, what, a.Reason),
					})
				}
				return "suggested " + a.Entry + " (" + what + "); it is installed only if the human approves. Carry on without it.", nil
			},
		},
	}
}
