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
	"github.com/drjzlyan/dhi/internal/agentkit/skills"
)

// Input is everything the composer needs; library lookups happen
// outside so the composer stays pure (dangling refs just omit).
type Input struct {
	AgentID        string
	Workspace      string // workspace name for {{workspace}}
	ManifestSystem string
	Role           *library.Role    // nil = no role
	Persona        *library.Persona // nil = no persona (F-045)
	Skills         []library.Skill  // in manifest order
	// Mode is the turn's working mode (F-144): "" is the normal write turn
	// and adds nothing, so its prompt is unchanged.
	Mode string
}

// Turn modes (F-144). plan, review and ask run read-only.
const (
	ModePlan   = "plan"
	ModeReview = "review"
	ModeAsk    = "ask"
)

// modeText is the instruction block each mode appends after the persona.
var modeText = map[string]string{
	ModePlan: "MODE: plan. This turn is read-only: you may read and search, but you cannot change anything, " +
		"and you must not try. Investigate what the request touches, then publish a plan by calling plan_update " +
		"once with the whole plan: 3 to 10 short steps, each a concrete change or check, every status todo. " +
		"Name the files or areas each step touches in its title. Then reply with two or three sentences " +
		"on the approach and the main risk. The human approves the plan before any change is made.",
	ModeReview: "MODE: review. This turn is read-only: read the change or code you are pointed at and report findings. " +
		"List problems first, most serious first, each with file and line and why it matters; then what you checked " +
		"and found sound. Do not fix anything and do not edit files.",
	ModeAsk: "MODE: ask. This turn is read-only: answer the question from the code and docs. " +
		"Say where the answer comes from (file and line). If you do not know, say so rather than guess. " +
		"Do not edit files or propose a plan unless asked.",
}

// ModeReadOnly reports whether mode is one of the read-only modes.
func ModeReadOnly(mode string) bool { _, ok := modeText[mode]; return ok }

// Compose renders the effective system block:
//
//	manifest system → role template → persona voice → skill catalog (manifest order)
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
	add(in.Persona.Render()) // role says what the job is, persona how they speak
	// Skills are disclosed progressively (agentskills.io, ADR-0043): a
	// catalog of the assigned skills; the agent loads one when a task
	// matches, instead of every body riding in every prompt.
	add(skills.Catalog(in.Skills))
	add(modeText[in.Mode])
	return strings.Join(parts, "\n\n")
}

// substitute expands the role template tokens.
func substitute(tpl string, in Input) string {
	out := strings.ReplaceAll(tpl, "{{agent}}", in.AgentID)
	return strings.ReplaceAll(out, "{{workspace}}", in.Workspace)
}

// ResolvePersona looks up the manifest's persona; a dangling or absent
// reference is nil (doctor names it).
func ResolvePersona(m *manifest.Agent, lib *library.Store) *library.Persona {
	if lib == nil || m == nil || m.Persona == "" {
		return nil
	}
	p, _ := lib.Persona(m.Persona)
	return p
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
