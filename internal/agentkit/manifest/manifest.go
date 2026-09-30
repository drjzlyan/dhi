// Package manifest defines DHI's agent roster format: one TOML document
// per agent under .dhi/agents/<id>.toml. Manifests declare identity,
// model, system prompt, tool allowlist, an embedded sandbox policy
// (validated through internal/sandbox), and the host CLI runtime the
// agent thinks through. Parsing is strict: unknown keys are errors so
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
	"time"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
	"github.com/drjzlyan/dhi/internal/sandbox"
)

// SchemaVersion is the agent manifest schema this build WRITES
// (F-031: + workflow). Parse still accepts schema 1 (no role/skills),
// schema 2 (role/skills, `runtime`), 3 (engine), and 4 (scopes) for
// back-compat — such files load with their engine derived from
// `runtime` — but Marshal always emits the current version.
const SchemaVersion = 5

// EngineCLIPrefix marks a CLI engine declaration: the host CLI is the
// inference engine (ADR-0019).
const EngineCLIPrefix = "cli:"

// EngineString renders a CLI name as an engine declaration.
func EngineString(cliName string) string { return EngineCLIPrefix + cliName }

// ParseEngine validates an engine declaration ("cli:<name>") and returns
// the CLI name. The empty string is a valid engine meaning "inherit the
// workspace default". An unknown CLI, an unknown kind, or the not-built
// `api:` kind refuses by name (ADR-0011).
func ParseEngine(engine string) (string, error) {
	e := strings.TrimSpace(strings.ToLower(engine))
	if e == "" {
		return "", nil
	}
	kind, name, ok := strings.Cut(e, ":")
	if !ok || name == "" {
		return "", fmt.Errorf("engine %q must be \"cli:<name>\"", engine)
	}
	switch kind {
	case "cli":
		if !clirun.IsCLIRuntime(name) {
			return "", fmt.Errorf("engine %q: unknown CLI (valid: %s)",
				engine, strings.Join(clirun.CLINames(), ", "))
		}
		return name, nil
	case "api":
		return "", fmt.Errorf("engine %q: the api engine kind is not built yet (use cli:<name>)", engine)
	default:
		return "", fmt.Errorf("engine %q: unknown kind %q (want cli)", engine, kind)
	}
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	mcpToolRe = regexp.MustCompile(`^mcp__[a-z0-9_]+__[a-z0-9_]+$`)
)

// BuiltinTools are the native tool names a manifest may allowlist.
// Any other entry must be an mcp__<server>__<tool> reference resolved
// against connected MCP servers at runtime.
// actions (task cards + PRs) are builtins too — the work-facing moves
// a human makes by hand. The F-028 served tools (tasks/KB/memory/
// channels/workspace search) are builtins as well: a manifest may
// allowlist them now, and turn time refuses by name until/unless the
// serving substrate actually offers them (ADR-0017).
var BuiltinTools = []string{
	"read", "write", "patch", "list", "glob", "search",
	"git_status", "git_log", "git_branch", "git_diff", "git_commit", "git_push",
	"task_create", "task_status", "task_assign", "pr_open",
	"task_list", "kb_search", "kb_contribute",
	"memory_append", "memory_read_notes", "memory_write_notes",
	"channel_post", "channel_read", "workspace_search",
	"ideation_list", "ideation_read", "session_read",
	"artifact_create", "artifact_edit", "propose_session",
	"run", "ask_human",
	"editor_open", "editor_reveal", "editor_apply_edit",
	"lsp_hover", "lsp_definition", "lsp_references", "lsp_rename", "lsp_code_action",
}

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

	// Role and Skills reference the behaviour library (F-027/ADR-0016).
	// Role is one slug (the job description: tools/policy defaults +
	// system template); Skills are attached instruction documents.
	// Dangling references load and turn fine — doctor warns by name.
	Role   string
	Skills []string

	// Engine is the full engine declaration (ADR-0019), e.g.
	// "cli:claude". Empty means "inherit the workspace default engine".
	// Runtime is the resolved CLI name (the convenience consumers use);
	// it mirrors Engine and is empty exactly when Engine is empty.
	Engine  string // "cli:<name>" or "" (inherit default)
	Runtime string // resolved CLI name ("claude"); "" = inherit default

	// Scopes is the agent's declared capability set (F-030 P2); nil/empty
	// falls back to scopes.Default() at enforcement time.
	Scopes scopes.Set

	// Workflow names the agent's default feature workflow slug (F-031);
	// empty inherits the team/workspace default, then builtin `feature`.
	Workflow string

	Timeout time.Duration // max wall time per run; 0 = no limit
	Retries int           // retries on transient CLI failure; 0 = none

	policy *sandbox.Policy // parsed from policy_json; nil if absent
}

// Policy returns the agent's sandbox policy, or nil when the manifest
// declares none (the runtime then applies a deny-all default).
func (a *Agent) Policy() *sandbox.Policy { return a.policy }

// UsesCLIRuntime reports whether the agent runs on a host CLI rather
// than the in-house engine.
func (a *Agent) UsesCLIRuntime() bool { return clirun.IsCLIRuntime(a.Runtime) }

// file is the on-disk TOML shape of one agent manifest.
type file struct {
	Schema    int               `toml:"schema"`
	Name      string            `toml:"name"`
	Model     string            `toml:"model"`
	System    string            `toml:"system"`
	Tools     []string          `toml:"tools"`
	Role      string            `toml:"role"`
	Skills    []string          `toml:"skills"`
	PolicyRaw string            `toml:"policy_json"`
	Engine    string            `toml:"engine"`
	Scopes    map[string]string `toml:"scopes"`
	Workflow  string            `toml:"workflow"`
	Runtime   string            `toml:"runtime"`
	Timeout   string            `toml:"timeout"`
	Retries   int               `toml:"retries"`
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
	if f.Schema < 1 || f.Schema > SchemaVersion {
		return nil, fmt.Errorf("agentkit/manifest: %s: schema %d, want 1..%d", id, f.Schema, SchemaVersion)
	}
	a := &Agent{
		ID:      id,
		Name:    strings.TrimSpace(f.Name),
		Model:   strings.TrimSpace(f.Model),
		System:  f.System,
		Tools:   f.Tools,
		Role:    strings.TrimSpace(f.Role),
		Skills:  f.Skills,
		Retries: f.Retries,
	}
	if f.Schema == 1 && (a.Role != "" || len(a.Skills) > 0) {
		return nil, fmt.Errorf("agentkit/manifest: %s: role/skills require schema = 2", id)
	}
	if a.Name == "" {
		return nil, fmt.Errorf("agentkit/manifest: %s: name is required", id)
	}
	if a.Model == "" {
		return nil, fmt.Errorf("agentkit/manifest: %s: model is required", id)
	}
	if a.Role != "" && !idRe.MatchString(a.Role) {
		return nil, fmt.Errorf("agentkit/manifest: %s: role %q is not a library slug (lowercase [a-z0-9._-])", id, a.Role)
	}
	seenSkill := map[string]bool{}
	for i, s := range a.Skills {
		s = strings.TrimSpace(s)
		if !idRe.MatchString(s) {
			return nil, fmt.Errorf("agentkit/manifest: %s: skills[%d]: %q is not a library slug (lowercase [a-z0-9._-])", id, i, s)
		}
		if seenSkill[s] {
			return nil, fmt.Errorf("agentkit/manifest: %s: duplicate skill %q", id, s)
		}
		seenSkill[s] = true
		a.Skills[i] = s
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
	declared, err := parseScopes(f.Scopes)
	if err != nil {
		return nil, fmt.Errorf("agentkit/manifest: %s: %w", id, err)
	}
	a.Scopes = declared
	if f.Schema < 4 && len(f.Scopes) > 0 {
		return nil, fmt.Errorf("agentkit/manifest: %s: scopes require schema = %d", id, SchemaVersion)
	}
	a.Workflow = strings.TrimSpace(f.Workflow)
	if a.Workflow != "" {
		if !idRe.MatchString(a.Workflow) {
			return nil, fmt.Errorf("agentkit/manifest: %s: workflow %q is not a slug (lowercase [a-z0-9._-])", id, a.Workflow)
		}
		if f.Schema < 5 {
			return nil, fmt.Errorf("agentkit/manifest: %s: workflow requires schema = %d", id, SchemaVersion)
		}
	}
	cliName, err := resolveEngine(f)
	if err != nil {
		return nil, fmt.Errorf("agentkit/manifest: %s: %w", id, err)
	}
	a.Runtime = cliName
	if cliName != "" {
		a.Engine = EngineString(cliName)
	}
	if f.Timeout != "" {
		d, err := time.ParseDuration(f.Timeout)
		if err != nil {
			return nil, fmt.Errorf("agentkit/manifest: %s: timeout %q: %w", id, f.Timeout, err)
		}
		a.Timeout = d
	}
	if a.Retries < 0 {
		return nil, fmt.Errorf("agentkit/manifest: %s: retries %d must not be negative", id, a.Retries)
	}
	return a, nil
}

// parseScopes validates the manifest's declared scope effects.
func parseScopes(raw map[string]string) (scopes.Set, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := scopes.Set{}
	for name, eff := range raw {
		sc, err := scopes.ParseScope(name)
		if err != nil {
			return nil, err
		}
		e, err := scopes.ParseEffect(eff)
		if err != nil {
			return nil, err
		}
		out[sc] = e
	}
	return out, nil
}

// resolveEngine maps the on-disk engine/runtime declarations to a
// resolved CLI name ("" = inherit the workspace default).
//
//   - schema 1/2: `runtime` is required and must be a registered CLI
//     (F-013 contract, preserved); `engine` refuses.
//   - schema 3: `engine = "cli:<name>"` is the declaration; the legacy
//     `runtime` is accepted as a synonym, but setting both refuses
//     (ambiguous). Neither set = inherit the workspace default.
func resolveEngine(f file) (string, error) {
	eng := strings.TrimSpace(f.Engine)
	run := strings.TrimSpace(strings.ToLower(f.Runtime))
	if f.Schema < 3 {
		if eng != "" {
			return "", fmt.Errorf("engine requires schema = %d", SchemaVersion)
		}
		if run == "" {
			return "", fmt.Errorf("runtime is required (schema %d)", f.Schema)
		}
		if !clirun.IsCLIRuntime(run) {
			return "", fmt.Errorf("runtime %q is not a registered host CLI (valid: %s)",
				run, strings.Join(clirun.CLINames(), ", "))
		}
		return run, nil
	}
	switch {
	case eng != "" && run != "":
		return "", fmt.Errorf("set engine or runtime, not both")
	case eng != "":
		return ParseEngine(eng)
	case run != "":
		return ParseEngine(EngineString(run))
	default:
		return "", nil // inherit the workspace default engine
	}
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
	f.Role = a.Role
	f.Skills = append([]string(nil), a.Skills...)
	// Emit the engine; derive it from Runtime when only the CLI name is
	// set (callers that predate the engine field keep working).
	switch {
	case a.Engine != "":
		f.Engine = a.Engine
	case a.Runtime != "":
		f.Engine = EngineString(a.Runtime)
	}
	if len(a.Scopes) > 0 {
		f.Scopes = map[string]string{}
		for sc, eff := range a.Scopes {
			f.Scopes[string(sc)] = string(eff)
		}
	}
	f.Workflow = a.Workflow
	f.Retries = a.Retries
	if a.Timeout > 0 {
		f.Timeout = a.Timeout.String()
	}
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
	// Callers may set only Runtime (the CLI name) or only Engine; the
	// round trip always yields both, so compare against the derived pair.
	wantEngine := a.Engine
	if wantEngine == "" && a.Runtime != "" {
		wantEngine = EngineString(a.Runtime)
	}
	wantRuntime := a.Runtime
	if wantRuntime == "" && a.Engine != "" {
		if name, perr := ParseEngine(a.Engine); perr == nil {
			wantRuntime = name
		}
	}
	if back.Name != a.Name || back.Model != a.Model || back.System != a.System ||
		strings.Join(back.Tools, ",") != strings.Join(a.Tools, ",") ||
		back.Role != a.Role || strings.Join(back.Skills, ",") != strings.Join(a.Skills, ",") ||
		back.Engine != wantEngine || back.Runtime != wantRuntime ||
		!scopesEqual(back.Scopes, a.Scopes) ||
		back.Timeout != a.Timeout || back.Retries != a.Retries {
		return nil, fmt.Errorf("agentkit/manifest: %s: marshal round-trip mismatch", a.ID)
	}
	return data.Bytes(), nil
}

func scopesEqual(a, b scopes.Set) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
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
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("agentkit/manifest: write %s: %w", a.ID, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("agentkit/manifest: write %s: %w", a.ID, err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
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
