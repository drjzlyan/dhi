// Package forge holds the tools agents build for themselves (F-069,
// ADR-0034). An agent that keeps doing the same thing proposes a tool; a
// human approves it; from then on every agent can call it as
// custom__<name> over the MCP loopback. A tool is either a recipe — steps
// of allowlisted commands with templated inputs, no shell — or a script
// run through the same sandboxed runner as skill_run. Agent-made tools
// may declare only read, write, exec or git scope: never push, network or
// admin. Nothing is active until a human approves it, and every edit is a
// new version that is approved again.
package forge

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the tool file schema.
const SchemaVersion = 1

// Prefix names custom tools on the served surface.
const Prefix = "custom__"

// Input is one typed argument of a tool.
type Input struct {
	Name        string `toml:"name"`
	Type        string `toml:"type"` // string | integer | boolean
	Description string `toml:"description"`
	Required    bool   `toml:"required"`
}

// Step is one recipe step: an allowlisted program argv whose elements
// may contain {{input}} templates.
type Step struct {
	Run []string `toml:"run"`
}

// Tool is one forged tool definition.
type Tool struct {
	Schema      int     `toml:"schema"`
	Name        string  `toml:"name"`
	Description string  `toml:"description"`
	Scope       string  `toml:"scope"`
	Inputs      []Input `toml:"input"`
	Steps       []Step  `toml:"step"`
	// Script is an executable body (with a #! line); inputs arrive as
	// positional args in declaration order.
	Script string `toml:"script,omitempty"`

	Version    int       `toml:"version"`
	ProposedBy string    `toml:"proposed_by"`
	Rationale  string    `toml:"rationale"`
	Proposed   time.Time `toml:"proposed"`
	ApprovedBy string    `toml:"approved_by,omitempty"`
	Approved   time.Time `toml:"approved,omitzero"`
}

// Kind says how the tool runs.
func (t Tool) Kind() string {
	if t.Script != "" {
		return "script"
	}
	return "recipe"
}

var (
	nameRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
	templateRe = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]*)\s*\}\}`)
)

// allowedScopes are what an agent-made tool may declare.
var allowedScopes = map[string]bool{"read": true, "write": true, "exec": true, "git": true}

// Validate checks a definition against the forge rules. allowed reports
// whether a program (argv[0]) and its first argument are on the run
// allowlist; nil skips that check (the store has no allowlist; serving
// re-checks at every call).
func (t Tool) Validate(allowed func(argv []string) error) error {
	switch {
	case !nameRe.MatchString(t.Name):
		return fmt.Errorf("forge: name %q: 2..32 of [a-z0-9_], starting with a letter", t.Name)
	case strings.TrimSpace(t.Description) == "":
		return fmt.Errorf("forge: %s: a description is required (it is what other agents read)", t.Name)
	case !allowedScopes[t.Scope]:
		return fmt.Errorf("forge: %s: scope %q — agent-made tools declare read, write, exec or git", t.Name, t.Scope)
	case (len(t.Steps) == 0) == (t.Script == ""):
		return fmt.Errorf("forge: %s: exactly one of steps or script", t.Name)
	case len(t.Steps) > 8:
		return fmt.Errorf("forge: %s: at most 8 steps", t.Name)
	case t.Script != "" && !strings.HasPrefix(t.Script, "#!"):
		return fmt.Errorf("forge: %s: a script starts with a #! line", t.Name)
	case t.Script != "" && t.Scope != "exec":
		return fmt.Errorf("forge: %s: a script tool has scope exec", t.Name)
	}
	declared := map[string]bool{}
	for _, in := range t.Inputs {
		if !nameRe.MatchString(in.Name) || declared[in.Name] {
			return fmt.Errorf("forge: %s: input %q is not a unique [a-z0-9_] name", t.Name, in.Name)
		}
		switch in.Type {
		case "string", "integer", "boolean":
		default:
			return fmt.Errorf("forge: %s: input %s type %q (want string, integer or boolean)", t.Name, in.Name, in.Type)
		}
		declared[in.Name] = true
	}
	for i, st := range t.Steps {
		if len(st.Run) == 0 {
			return fmt.Errorf("forge: %s: step %d is empty", t.Name, i+1)
		}
		if templateRe.MatchString(st.Run[0]) {
			return fmt.Errorf("forge: %s: step %d: the program cannot be templated", t.Name, i+1)
		}
		for _, a := range st.Run {
			for _, m := range templateRe.FindAllStringSubmatch(a, -1) {
				if !declared[m[1]] {
					return fmt.Errorf("forge: %s: step %d uses {{%s}}, which is not a declared input", t.Name, i+1, m[1])
				}
			}
		}
		if allowed != nil {
			if err := allowed(st.Run); err != nil {
				return fmt.Errorf("forge: %s: step %d: %w", t.Name, i+1, err)
			}
		}
	}
	return nil
}

// Args binds call arguments to the declared inputs: required present,
// types right, no unknown keys. Values become strings for templating.
func (t Tool) Args(raw map[string]any) (map[string]string, error) {
	out := map[string]string{}
	known := map[string]Input{}
	for _, in := range t.Inputs {
		known[in.Name] = in
	}
	for k := range raw {
		if _, ok := known[k]; !ok {
			return nil, fmt.Errorf("unknown argument %q", k)
		}
	}
	for _, in := range t.Inputs {
		v, ok := raw[in.Name]
		if !ok {
			if in.Required {
				return nil, fmt.Errorf("argument %q is required", in.Name)
			}
			continue
		}
		switch in.Type {
		case "string":
			s, ok := v.(string)
			if !ok || strings.ContainsAny(s, "\n\r\x00") {
				return nil, fmt.Errorf("argument %q must be a single-line string", in.Name)
			}
			out[in.Name] = s
		case "integer":
			f, ok := v.(float64)
			if !ok || f != float64(int64(f)) {
				return nil, fmt.Errorf("argument %q must be an integer", in.Name)
			}
			out[in.Name] = strconv.FormatInt(int64(f), 10)
		case "boolean":
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("argument %q must be true or false", in.Name)
			}
			out[in.Name] = strconv.FormatBool(b)
		}
	}
	return out, nil
}

// Expand renders the recipe's argvs for bound args. Templates are
// replaced in place, so an argument never splits into several argv
// elements; an argument that templates an omitted optional input is
// dropped whole ("-count={{count}}" vanishes, never "-count=").
func (t Tool) Expand(args map[string]string) [][]string {
	var out [][]string
	for _, st := range t.Steps {
		var argv []string
		for _, a := range st.Run {
			missing := false
			v := templateRe.ReplaceAllStringFunc(a, func(m string) string {
				val, ok := args[templateRe.FindStringSubmatch(m)[1]]
				missing = missing || !ok
				return val
			})
			if missing {
				continue
			}
			argv = append(argv, v)
		}
		out = append(out, argv)
	}
	return out
}

// ScriptArgs are the script's positional args, in input order.
func (t Tool) ScriptArgs(args map[string]string) []string {
	var out []string
	for _, in := range t.Inputs {
		out = append(out, args[in.Name])
	}
	return out
}

// InputSchema renders the JSON schema served over MCP.
func (t Tool) InputSchema() string {
	var props, req []string
	for _, in := range t.Inputs {
		props = append(props, fmt.Sprintf("%q:{\"type\":%q,\"description\":%q}", in.Name, in.Type, in.Description))
		if in.Required {
			req = append(req, strconv.Quote(in.Name))
		}
	}
	return fmt.Sprintf(`{"type":"object","properties":{%s},"required":[%s],"additionalProperties":false}`,
		strings.Join(props, ","), strings.Join(req, ","))
}

// Store keeps tools under .dhi/tools: <name>.toml (approved), pending/,
// archived/, scripts in <name>.script, and usage counts in usage.toml.
type Store struct {
	root string
	mu   sync.Mutex
	now  func() time.Time
}

// Open roots the store at wsRoot.
func Open(wsRoot string) *Store {
	return &Store{root: filepath.Join(wsRoot, ".dhi", "tools"), now: time.Now}
}

func (s *Store) active(name string) string  { return filepath.Join(s.root, name+".toml") }
func (s *Store) pending(name string) string { return filepath.Join(s.root, "pending", name+".toml") }
func (s *Store) script(name string) string  { return filepath.Join(s.root, name+".script") }

// ScriptPath is where an approved script tool's body lives.
func (s *Store) ScriptPath(name string) string { return s.script(name) }

func read(p string) (Tool, error) {
	var t Tool
	md, err := toml.DecodeFile(p, &t)
	if err != nil {
		return Tool{}, fmt.Errorf("forge: %s: %w", p, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return Tool{}, fmt.Errorf("forge: %s: unknown key %q", p, und[0].String())
	}
	return t, nil
}

func write(p string, t Tool) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("forge: %w", err)
	}
	var b strings.Builder
	b.WriteString("# DHI forged tool (F-069). Proposed by an agent; active only once approved.\n")
	if err := toml.NewEncoder(&b).Encode(t); err != nil {
		return fmt.Errorf("forge: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("forge: %w", err)
	}
	return os.Rename(tmp, p)
}

// Propose validates t and parks it for review, as the next version of
// any approved tool of that name. It replaces an earlier pending
// proposal of the same name.
func (s *Store) Propose(t Tool, allowed func([]string) error) (Tool, error) {
	t.Schema = SchemaVersion
	if err := t.Validate(allowed); err != nil {
		return Tool{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Version = 1
	if cur, err := read(s.active(t.Name)); err == nil {
		t.Version = cur.Version + 1
	}
	t.Proposed, t.ApprovedBy, t.Approved = s.now().UTC(), "", time.Time{}
	return t, write(s.pending(t.Name), t)
}

// Approve activates a pending proposal (script bodies are written as an
// executable beside it).
func (s *Store) Approve(name, by string) (Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := read(s.pending(name))
	if err != nil {
		return Tool{}, err
	}
	if err := t.Validate(nil); err != nil {
		return Tool{}, err
	}
	t.ApprovedBy, t.Approved = by, s.now().UTC()
	if t.Script != "" {
		if err := os.WriteFile(s.script(name), []byte(t.Script), 0o755); err != nil {
			return Tool{}, fmt.Errorf("forge: %w", err)
		}
	} else {
		_ = os.Remove(s.script(name))
	}
	if err := write(s.active(name), t); err != nil {
		return Tool{}, err
	}
	return t, os.Remove(s.pending(name))
}

// Reject drops a pending proposal.
func (s *Store) Reject(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.pending(name)); err != nil {
		return fmt.Errorf("forge: no pending tool %q", name)
	}
	return nil
}

// Archive deactivates an approved tool (kept under archived/).
func (s *Store) Archive(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dst := filepath.Join(s.root, "archived", name+".toml")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(s.active(name), dst); err != nil {
		return fmt.Errorf("forge: no active tool %q", name)
	}
	_ = os.Remove(s.script(name))
	return nil
}

func (s *Store) list(dir string) ([]Tool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("forge: %w", err)
	}
	var out []Tool
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" || e.Name() == "usage.toml" {
			continue
		}
		t, err := read(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Active lists approved tools.
func (s *Store) Active() ([]Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(s.root)
}

// Pending lists proposals awaiting review.
func (s *Store) Pending() ([]Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(filepath.Join(s.root, "pending"))
}

// Usage is how often each tool ran, and when last.
type Usage struct {
	Calls int       `toml:"calls"`
	Last  time.Time `toml:"last"`
}

// RecordUse counts one call (best-effort).
func (s *Store) RecordUse(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.usage()
	cur := u[name]
	cur.Calls++
	cur.Last = s.now().UTC()
	u[name] = cur
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return
	}
	var b strings.Builder
	if toml.NewEncoder(&b).Encode(u) == nil {
		_ = os.WriteFile(filepath.Join(s.root, "usage.toml"), []byte(b.String()), 0o644)
	}
}

// Usages returns the call counts.
func (s *Store) Usages() map[string]Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage()
}

func (s *Store) usage() map[string]Usage {
	u := map[string]Usage{}
	_, _ = toml.DecodeFile(filepath.Join(s.root, "usage.toml"), &u)
	return u
}
