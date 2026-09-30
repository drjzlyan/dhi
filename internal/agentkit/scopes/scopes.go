// Package scopes is DHI's capability model (F-030 P2, ADR-0019): an
// agent's authority is a set of scopes, each with an effect
// (auto | ask | deny). It replaces "the allowlist decides everything"
// with "the allowlist decides what a tool is, the scope decides what
// the agent may do with it". Resolution layers manifest → team →
// workspace, later wins; DHI's default is deny-by-default for anything
// outside the declared surface.
package scopes

import (
	"fmt"
	"strings"
)

// Scope is one capability class.
type Scope string

// The scope set (F-030).
const (
	Read    Scope = "read"
	Write   Scope = "write"
	Exec    Scope = "exec"
	Network Scope = "network"
	Git     Scope = "git"
	Push    Scope = "push"
	Admin   Scope = "admin"
)

// Effect is how a scope is enforced.
type Effect string

// Effects.
const (
	Auto Effect = "auto" // run without asking
	Ask  Effect = "ask"  // cross the approvals queue
	Deny Effect = "deny" // refuse by name
)

// Set maps scopes to effects.
type Set map[Scope]Effect

// ParseEffect validates an effect name.
func ParseEffect(s string) (Effect, error) {
	switch Effect(s) {
	case Auto, Ask, Deny:
		return Effect(s), nil
	default:
		return "", fmt.Errorf("unknown effect %q (want auto, ask, or deny)", s)
	}
}

// ParseScope validates a scope name.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case Read, Write, Exec, Network, Git, Push, Admin:
		return Scope(s), nil
	default:
		return "", fmt.Errorf("unknown scope %q (want read/write/exec/network/git/push/admin)", s)
	}
}

// Default is DHI's baseline: reads are free, mutations ask, and the
// dangerous scopes are denied until declared.
func Default() Set {
	return Set{
		Read: Auto, Write: Ask, Exec: Ask, Git: Ask,
		Push: Ask, Network: Deny, Admin: Deny,
	}
}

// EffectFor returns the effect for a scope ("" scope or unknown → deny).
func (s Set) EffectFor(sc Scope) Effect {
	if e, ok := s[sc]; ok {
		return e
	}
	return Deny
}

// Resolve overlays layers in order (later wins); a nil/empty layer
// leaves the prior value untouched.
func Resolve(layers ...Set) Set {
	out := Set{}
	for _, layer := range layers {
		for sc, ef := range layer {
			out[sc] = ef
		}
	}
	if len(out) == 0 {
		return Default()
	}
	// Fill gaps from the default so unspecified scopes stay safe.
	def := Default()
	for sc, ef := range def {
		if _, ok := out[sc]; !ok {
			out[sc] = ef
		}
	}
	return out
}

// ToolScope maps a served tool slug to its scope. A bridged third-party
// MCP tool (mcp__<server>__<tool>) maps to network — reaching an external
// server is deny-by-default and must be granted (ADR-0022). Any other
// unknown tool maps to admin (deny-by-default) so a new tool is never
// implicitly allowed.
func ToolScope(tool string) Scope {
	if sc, ok := toolScopes[tool]; ok {
		return sc
	}
	if strings.HasPrefix(tool, "mcp__") {
		return Network
	}
	return Admin
}

// toolScopes is the single source of truth for tool → scope. Memory
// tools map to read: the agent's journal/notes are private, not a
// shared-boundary mutation (the F-028 approvals precedent).
var toolScopes = map[string]Scope{
	"read": Read, "list": Read, "glob": Read, "workspace_search": Read,
	"search":     Read,
	"git_status": Read, "git_log": Read, "git_branch": Read, "git_diff": Read,
	"lsp_hover": Read, "lsp_definition": Read, "lsp_references": Read, "lsp_code_action": Read,
	"editor_open": Read, "editor_reveal": Read,
	"ideation_list": Read, "ideation_read": Read, "session_read": Read,
	"ask_human": Read,
	"task_list": Read, "kb_search": Read, "channel_read": Read,
	"memory_append": Read, "memory_read_notes": Read, "memory_write_notes": Read,

	"write": Write, "patch": Write, "editor_apply_edit": Write,
	"artifact_create": Write, "artifact_edit": Write,
	"task_create": Write, "task_status": Write, "task_assign": Write,
	"kb_contribute": Write, "channel_post": Write, "lsp_rename": Write,

	// propose_session records a pending request for the human to accept;
	// it is the agent's sanctioned voice, so it does not need its own
	// approval (the human decision is the proposal's resolution).
	"propose_session": Read,

	"git_commit": Git, "git_push": Push, "pr_open": Push, "run": Exec, "skill_run": Exec,
}
