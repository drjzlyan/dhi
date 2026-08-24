// Package manifest defines DHI's agent roster format: one TOML document
// per agent under .dhi/agents/<id>.toml. Manifests declare identity,
// model, system prompt, tool allowlist, an embedded sandbox policy
// (validated through internal/sandbox), and the env var holding the
// provider API key. Parsing is strict: unknown keys are errors so
// hand-edited rosters fail loudly instead of silently misbehaving.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

// SchemaVersion is the agent manifest schema this build understands.
const SchemaVersion = 1

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	envVarRe  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	mcpToolRe = regexp.MustCompile(`^mcp__[a-z0-9_]+__[a-z0-9_]+$`)
)

// BuiltinTools are the native tool names a manifest may allowlist.
// Any other entry must be an mcp__<server>__<tool> reference resolved
// against connected MCP servers at runtime.
var BuiltinTools = []string{"read", "write", "list", "search"}

// IsBuiltinTool reports whether name is one of the native tools.
func IsBuiltinTool(name string) bool {
	for _, b := range BuiltinTools {
		if b == name {
			return true
		}
	}
	return false
}

// ValidToolRef reports whether name is a well-formed tool reference:
// a builtin name or an mcp__<server>__<tool> reference.
func ValidToolRef(name string) bool {
	return IsBuiltinTool(name) || mcpToolRe.MatchString(name)
}

// Agent is one rostered agent.
type Agent struct {
	ID     string   // slug matching the roster filename stem
	Name   string   // display name
	Model  string   // provider model identifier
	System string   // system prompt ("")
	Tools  []string // allowlisted tool refs; empty allows none
	EnvVar string   // env var holding the provider API key ("" = none)

	policy *sandbox.Policy // parsed from policy_json; nil if absent
}

// Policy returns the agent's sandbox policy, or nil when the manifest
// declares none (the runtime then applies a deny-all default).
func (a *Agent) Policy() *sandbox.Policy { return a.policy }

// file is the on-disk TOML shape of one agent manifest.
type file struct {
	Schema    int      `toml:"schema"`
	Name      string   `toml:"name"`
	Model     string   `toml:"model"`
	System    string   `toml:"system"`
	Tools     []string `toml:"tools"`
	PolicyRaw string   `toml:"policy_json"`
	EnvVar    string   `toml:"env_var"`
}

// Parse decodes and strictly validates one agent manifest. The id comes
// from outside the document (the roster filename stem) so renames cannot
// desynchronize identity from storage location.
func Parse(id string, data []byte) (*Agent, error) {
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("agentkit/manifest: bad agent id %q (lowercase [a-z0-9._-])", id)
	}
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, fmt.Errorf("agentkit/manifest: %s: %w", id, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("agentkit/manifest: %s: unknown key(s): %s", id, strings.Join(keys, ", "))
	}
	if f.Schema != SchemaVersion {
		return nil, fmt.Errorf("agentkit/manifest: %s: schema %d, want %d", id, f.Schema, SchemaVersion)
	}
	a := &Agent{
		ID:     id,
		Name:   strings.TrimSpace(f.Name),
		Model:  strings.TrimSpace(f.Model),
		System: f.System,
		Tools:  f.Tools,
		EnvVar: f.EnvVar,
	}
	if a.Name == "" {
		return nil, fmt.Errorf("agentkit/manifest: %s: name is required", id)
	}
	if a.Model == "" {
		return nil, fmt.Errorf("agentkit/manifest: %s: model is required", id)
	}
	seen := map[string]bool{}
	for i, t := range a.Tools {
		t = strings.TrimSpace(t)
		if !ValidToolRef(t) {
			return nil, fmt.Errorf("agentkit/manifest: %s: tools[%d]: unknown tool %q (builtins: %s; or mcp__<server>__<tool>)",
				id, i, t, strings.Join(BuiltinTools, ", "))
		}
		if seen[t] {
			return nil, fmt.Errorf("agentkit/manifest: %s: duplicate tool %q", id, t)
		}
		seen[t] = true
		a.Tools[i] = t
	}
	if f.PolicyRaw != "" {
		p, err := sandbox.ParsePolicy([]byte(f.PolicyRaw))
		if err != nil {
			return nil, fmt.Errorf("agentkit/manifest: %s: policy_json: %w", id, err)
		}
		a.policy = p
	}
	if a.EnvVar != "" && !envVarRe.MatchString(a.EnvVar) {
		return nil, fmt.Errorf("agentkit/manifest: %s: env_var %q is not a valid env var name", id, a.EnvVar)
	}
	return a, nil
}

// LoadDir loads every *.toml under dir as one agent, requiring each
// filename stem to match its declared identity. The returned roster is
// sorted by ID for deterministic UI ordering. An empty or missing dir is
// a valid, empty roster.
func LoadDir(dir string) ([]*Agent, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agentkit/manifest: read %s: %w", dir, err)
	}
	var roster []*Agent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".toml")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("agentkit/manifest: read %s: %w", e.Name(), err)
		}
		a, err := Parse(id, data)
		if err != nil {
			return nil, err
		}
		roster = append(roster, a)
	}
	sort.Slice(roster, func(i, j int) bool { return roster[i].ID < roster[j].ID })
	return roster, nil
}
