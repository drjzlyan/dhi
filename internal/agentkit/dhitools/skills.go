package dhitools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/skills"
	"github.com/drjzlyan/dhi/internal/mcp"
)

// SkillHit is one plugin or skill source an agent can suggest (F-091,
// ADR-0043): a plugin from a configured marketplace, by name.
type SkillHit struct {
	Name, Version, Description string
}

// skillTools serves skill_search and skill_suggest (F-091, ADR-0043): an
// employee that would do better with a skill finds one — in the plugin
// marketplaces the human configured, or anywhere it knows a GitHub
// repository of Agent Skills — and suggests it; DHI pins and shows it in
// full, and the human decides.
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
				Description: "Search the plugin marketplaces DHI is set up with for skills. Args: {\"query\":\"terraform\"} (empty lists all). Returns plugin names to pass to skill_suggest.",
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
					return "no plugins match; suggest a GitHub repository of skills you know instead", nil
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
				Description: "Suggest installing an Agent Skill when it would make you better at this kind of task. " +
					"Args: {\"entry\":\"a plugin name from skill_search, or a GitHub URL / owner/repo of skills\",\"reason\":\"what it would help with\"}. DHI pins the exact commit " +
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

// assigned reports whether skill name is one of the calling agent's.
func (d Deps) assigned(name string) bool {
	if d.Agent == nil {
		return false
	}
	for _, s := range d.Agent.Skills {
		if s == name {
			return true
		}
	}
	return false
}

// activateTool serves skill_activate (agentskills.io, ADR-0043): it hands
// the agent one of its skills' instructions, wrapped, with the skill's
// directory and bundled files listed, for runtimes that cannot read the
// SKILL.md the catalog points at. Only the agent's own skills are
// offered (the name is an enum); none, no tool.
func (d Deps) activateTool() []tool {
	if d.Skills == nil || d.Agent == nil || len(d.Agent.Skills) == 0 {
		return nil
	}
	var names []string
	for _, n := range d.Agent.Skills {
		if _, ok := d.Skills.Skill(n); ok {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	enum, _ := json.Marshal(names)
	type activateArgs struct {
		Name string `json:"name"`
	}
	seen := map[string]bool{}
	return []tool{{
		info: mcp.ToolInfo{
			Name:        "skill_activate",
			Description: "Load the full instructions of one of your skills when a task matches its description. Args: {\"name\":\"<skill>\"}.",
			InputSchema: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string","enum":` + string(enum) + `}},"additionalProperties":false}`),
		},
		parse: func(raw json.RawMessage) (any, error) {
			var a activateArgs
			if err := args(raw, &a); err != nil {
				return nil, err
			}
			a.Name = strings.TrimSpace(a.Name)
			if !d.assigned(a.Name) {
				return nil, fmt.Errorf("%q is not one of your skills", a.Name)
			}
			return a, nil
		},
		exec: func(_ context.Context, dec any) (string, error) {
			name := dec.(activateArgs).Name
			k, ok := d.Skills.Skill(name)
			if !ok {
				return "", fmt.Errorf("skill %q is not installed", name)
			}
			if seen[name] { // already in context: do not repeat it
				return "skill " + name + " is already loaded above; follow it.", nil
			}
			seen[name] = true
			return skills.Activation(*k), nil
		},
	}}
}
