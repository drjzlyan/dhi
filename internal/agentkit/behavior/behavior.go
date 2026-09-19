// Package behavior composes an agent's effective system block from the
// behaviour library (F-027, ADR-0016). It is the single source of the
// persona assembly: manifest system + role template + attached skill
// bodies, with {{agent}}/{{workspace}} substitution — deterministic,
// pure, and shared by turns, the Settings preview, and doctor so the
// preview is byte-exact with what the agent receives.
package behavior

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
)

// Input is everything the composer needs; library lookups happen
// outside so the composer stays pure (dangling refs just omit).
type Input struct {
	AgentID        string
	Workspace      string // workspace name for {{workspace}}
	ManifestSystem string
	Role           *library.Role   // nil = no role
	Skills         []library.Skill // in manifest order
}

// Compose renders the effective system block:
//
//	manifest system → role template → skills (manifest order)
//
// Empty parts vanish (no stray blank sections); the whole result is
// trimmed. Skills extend the role, the role extends the manifest,
// standards stay the last layer (appended by the caller).
func Compose(in Input) string {
	var parts []string
	add := func(s string) {
		if t := strings.TrimSpace(s); t != "" {
			parts = append(parts, t)
		}
	}
	add(in.ManifestSystem)
	if in.Role != nil {
		add(substitute(in.Role.System, in))
	}
	for _, k := range in.Skills {
		add(k.Body)
	}
	return strings.Join(parts, "\n\n")
}

// substitute expands the role template tokens.
func substitute(tpl string, in Input) string {
	out := strings.ReplaceAll(tpl, "{{agent}}", in.AgentID)
	return strings.ReplaceAll(out, "{{workspace}}", in.Workspace)
}

// Resolve looks up the role + skills for a manifest against the
// library, dropping dangling references (the caller/doctor surfaces
// them — the composer stays total).
func Resolve(m *manifest.Agent, lib *library.Store) (*library.Role, []library.Skill) {
	if lib == nil || m == nil {
		return nil, nil
	}
	var role *library.Role
	if m.Role != "" {
		role, _ = lib.Role(m.Role)
	}
	var skills []library.Skill
	for _, slug := range m.Skills {
		if k, ok := lib.Skill(slug); ok {
			skills = append(skills, *k)
		}
	}
	return role, skills
}
