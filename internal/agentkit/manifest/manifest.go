// Package manifest defines DHI's agent roster format: one TOML document
// per agent under .dhi/agents/<id>.toml. Manifests declare identity,
// model, system prompt, tool allowlist, an embedded sandbox policy
// (validated through internal/sandbox), and the env var holding the
// provider API key. Parsing is strict: unknown keys are errors so
// hand-edited rosters fail loudly instead of silently misbehaving.
package manifest

import (
	"bytes"
	"encoding/json"
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

// Marshal renders a validated agent back to strict manifest TOML. The
// round trip through Parse guarantees what we write is what we would
// accept, so a future reader can never disagree with the writer.
func Marshal(a *Agent) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("agentkit/manifest: marshal nil agent")
	}
	var f file
	f.Schema = SchemaVersion
	f.Name = a.Name
	f.Model = a.Model
	f.System = a.System
	f.Tools = append([]string(nil), a.Tools...)
	f.EnvVar = a.EnvVar
	if a.policy != nil {
		raw, err := json.Marshal(a.policy)
		if err != nil {
			return nil, fmt.Errorf("agentkit/manifest: %s: policy: %w", a.ID, err)
		}
		f.PolicyRaw = string(raw)
	}
	data := new(bytes.Buffer)
	if err := toml.NewEncoder(data).Encode(f); err != nil {
		return nil, fmt.Errorf("agentkit/manifest: %s: encode: %w", a.ID, err)
	}
	back, err := Parse(a.ID, data.Bytes())
	if err != nil {
		return nil, fmt.Errorf("agentkit/manifest: %s: marshal self-check: %w", a.ID, err)
	}
	if back.Name != a.Name || back.Model != a.Model || back.System != a.System ||
		back.EnvVar != a.EnvVar || strings.Join(back.Tools, ",") != strings.Join(a.Tools, ",") {
		return nil, fmt.Errorf("agentkit/manifest: %s: marshal round-trip mismatch", a.ID)
	}
	return data.Bytes(), nil
}

// WriteFile validates-then-writes <dir>/<id>.toml atomically. The file
// stem is the identity; renames are Delete+Write at the caller level.
func WriteFile(dir string, a *Agent) error {
	data, err := Marshal(a)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, a.ID+".toml")
	tmp, err := os.CreateTemp(dir, "."+a.ID+"-*.toml")
	if err != nil {
		return fmt.Errorf("agentkit/manifest: write %s: %w", a.ID, err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("agentkit/manifest: write %s: %w", a.ID, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("agentkit/manifest: write %s: %w", a.ID, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("agentkit/manifest: write %s: %w", a.ID, err)
	}
	return nil
}

// ArchiveDirName is where archived manifests live inside the roster
// directory. LoadDir skips directories, so archived agents drop out of
// every roster read while staying on disk for restoration.
const ArchiveDirName = ".archived"

// Archive moves <dir>/<id>.toml to <dir>/.archived/<id>.toml.
func Archive(dir, id string) error {
	src := filepath.Join(dir, id+".toml")
	dstDir := filepath.Join(dir, ArchiveDirName)
	dst := filepath.Join(dstDir, id+".toml")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("agentkit/manifest: archive %s: %w", id, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("agentkit/manifest: archive: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("agentkit/manifest: archive %s: %w", id, err)
	}
	return nil
}

// Restore moves an archived manifest back into the active roster.
func Restore(dir, id string) error {
	src := filepath.Join(dir, ArchiveDirName, id+".toml")
	dst := filepath.Join(dir, id+".toml")
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("agentkit/manifest: restore %s: %q already active", id, id)
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("agentkit/manifest: restore %s: %w", id, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("agentkit/manifest: restore %s: %w", id, err)
	}
	return nil
}

// ArchivedIDs lists archived agent ids sorted (filename stems that parse
// cleanly; broken entries surface in doctor instead).
func ArchivedIDs(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(dir, ArchiveDirName))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".toml"))
	}
	sort.Strings(out)
	return out
}

// ReadArchived parses one archived manifest by id (for inspection UIs).
func ReadArchived(dir, id string) (*Agent, error) {
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("agentkit/manifest: bad agent id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(dir, ArchiveDirName, id+".toml"))
	if err != nil {
		return nil, fmt.Errorf("agentkit/manifest: read archived %s: %w", id, err)
	}
	return Parse(id, data)
}
