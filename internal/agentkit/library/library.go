// Package library is DHI's behaviour library (F-027, ADR-0016): the
// predefined format for agent people. Roles are job descriptions
// (default tools, policy preset, system template); skills are Agent
// Skills (agentskills.io, ADR-0043): SKILL.md directories read by
// internal/agentkit/skills. The built-in set ships embedded in the
// binary; skills you install, a workspace's own, and those other tools
// installed (~/.agents/skills, a member repo's .agents/skills or
// .claude/skills) shadow built-ins by name. Malformed cards become named
// warnings — the store stays usable, nothing is silently dropped.
package library

import (
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/agentkit/skills"
	"github.com/drjzlyan/dhi/internal/workspace"
)

//go:embed builtin
var builtinFS embed.FS

// Role is one job description.
type Role struct {
	Slug        string
	Description string
	Tools       []string // default allowlist prefilled at create time
	Preset      string   // policy preset: "" | read-only | read-write
	System      string   // system template ({{agent}}, {{workspace}})
}

// Skill is one Agent Skill (ADR-0043). Its scripts (scripts/ in the
// skill directory) run directly (shebang, no shell) under the OS sandbox
// through the served skill_run tool, network denied unless the
// exec/network scope says otherwise.
type Skill = skills.Skill

// Entry is one library listing row.
type Entry struct {
	Slug        string
	Kind        string // "role" | "skill"
	Source      string // "builtin" | "local"
	Description string
}

// policy presets — the canonical JSON the role's preset names. The
// agent form (P5) offers the same presets; raw JSON stays hand-edit
// territory.
const (
	PresetReadOnly  = "read-only"
	PresetReadWrite = "read-write"
)

// PolicyPresetJSON maps a preset slug to its canonical policy JSON.
// An unknown preset is a named error, never a guessed default.
func PolicyPresetJSON(preset string) (string, error) {
	switch preset {
	case "":
		return "", nil
	case PresetReadOnly:
		return `{"rules":[` +
			`{"op":"read","path":"**","effect":"allow"},` +
			`{"op":"write","path":"**","effect":"deny"},` +
			`{"op":"exec","path":"**","effect":"deny"},` +
			`{"op":"net","path":"**","effect":"deny"}]}`, nil
	case PresetReadWrite:
		return `{"rules":[` +
			`{"op":"read","path":"**","effect":"allow"},` +
			`{"op":"write","path":"**","effect":"allow"},` +
			`{"op":"exec","path":"**","effect":"deny"},` +
			`{"op":"net","path":"**","effect":"deny"}]}`, nil
	}
	return "", fmt.Errorf("library: unknown policy preset %q (valid: %s, %s)", preset, PresetReadOnly, PresetReadWrite)
}

// roleFile is the on-disk TOML shape of one role card.
type roleFile struct {
	Schema      int      `toml:"schema"`
	Description string   `toml:"description"`
	Tools       []string `toml:"tools"`
	Preset      string   `toml:"policy_preset"`
	System      string   `toml:"system"`
}

// validSlug mirrors the manifest id grammar.
func validSlug(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '.' || c == '_' || c == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

// parseRole decodes one role card from bytes.
func parseRole(slug string, data []byte) (*Role, error) {
	var f roleFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, err
	}
	if und := md.Undecoded(); len(und) > 0 {
		var keys []string
		for _, k := range und {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
	}
	if f.Schema != 1 {
		return nil, fmt.Errorf("schema %d, want 1", f.Schema)
	}
	r := &Role{
		Slug:        slug,
		Description: strings.TrimSpace(f.Description),
		Tools:       f.Tools,
		Preset:      strings.TrimSpace(f.Preset),
		System:      f.System,
	}
	if r.Description == "" {
		return nil, fmt.Errorf("description is required")
	}
	if r.Preset != "" {
		if _, err := PolicyPresetJSON(r.Preset); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// ParseRole validates one role card's bytes without writing it (packs
// pre-validate every card before the first file lands, ADR-0022).
func ParseRole(slug string, data []byte) (*Role, error) { return parseRole(slug, data) }

// WriteRole is the package-level writer (the Store method needs no
// state); packs call it after ParseRole.
func WriteRole(ws *workspace.Workspace, r *Role) error {
	var s Store
	return s.WriteRole(ws, r)
}

// WriteSkill is the package-level writer for skill directories.
func WriteSkill(ws *workspace.Workspace, k *Skill) error {
	var s Store
	return s.WriteSkill(ws, k)
}

// Skill directories, set once by the app at startup ("" — tests, tools —
// means none).
var (
	// UserSkillsDir is the skills you installed for every workspace:
	// <config>/dhi/skills.
	UserSkillsDir string
	// AgentsSkillsDir is ~/.agents/skills, the cross-tool convention:
	// skills other tools installed there are read too, never written.
	AgentsSkillsDir string
	// BuiltinSkillsDir is where the built-in skills are written out, so
	// agents can read their SKILL.md like any other (MaterializeBuiltins).
	BuiltinSkillsDir string
)

// Source labels: where a card was read from.
const (
	SourceBuiltin = "builtin"
	SourceYours   = "yours"  // UserSkillsDir, every workspace
	SourceAgents  = "agents" // ~/.agents/skills (another tool's install)
	SourceLocal   = "local"  // this workspace's .dhi/
	SourceMember  = "repo"   // a member repo's .agents/skills or .claude/skills
)

// Store is the merged library: embedded builtins shadowed by local
// cards. Open never fails — malformed local cards become warnings so
// the doctor names them and everything else keeps working.
type Store struct {
	roles    map[string]*Role
	skills   map[string]*Skill
	personas map[string]*Persona
	sources  map[string]string // slug+kind → source
	warnings []string
	root     string // workspace root ("" for a builtins-only store)
	fp       string // Fingerprint of the local cards when this snapshot was read
}

// Fingerprint summarises the local role, skill and persona cards (names,
// sizes, modification times). It changes whenever a card is added, edited or
// removed, which is how a long-running crew notices a library edit (F-052).
func Fingerprint(ws *workspace.Workspace) string {
	if ws == nil {
		return ""
	}
	var b strings.Builder
	dirs := []string{filepath.Join(ws.Root, workspace.DirRoles), filepath.Join(ws.Root, workspace.DirPersonas)}
	for _, dir := range append(dirs, skillDirs(ws)...) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		b.WriteString(dir + ":")
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if e.IsDir() { // a skill directory: its SKILL.md is what changes
				if fi, err := os.Stat(filepath.Join(dir, e.Name(), skills.File)); err == nil {
					info = fi
				}
			}
			fmt.Fprintf(&b, "%s|%d|%d;", e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// Stale reports whether the local cards changed since this snapshot was read.
func (s *Store) Stale(ws *workspace.Workspace) bool { return s.fp != Fingerprint(ws) }

// Open loads the merged library for ws (builtins + .dhi/roles +
// .dhi/skills). ws may be nil for a builtins-only store (tests).
func Open(ws *workspace.Workspace) *Store {
	s := &Store{roles: map[string]*Role{}, skills: map[string]*Skill{}, personas: map[string]*Persona{}, sources: map[string]string{}}
	s.fp = Fingerprint(ws) // before reading, so an edit mid-load is seen as stale next time
	s.loadPersonas(ws)
	if ws != nil {
		s.root = ws.Root
	}
	s.loadBuiltinDir("builtin/roles", ".toml", func(slug string, data []byte) error {
		r, err := parseRole(slug, data)
		if err != nil {
			return err
		}
		s.roles[r.Slug] = r
		s.sources["role/"+r.Slug] = "builtin"
		return nil
	})
	s.loadBuiltinSkills()
	s.loadSkillDir(AgentsSkillsDir, SourceAgents)
	s.loadSkillDir(UserSkillsDir, SourceYours)
	if ws != nil {
		s.loadLocalDir(filepath.Join(ws.Root, workspace.DirRoles), ".toml", "role",
			func(slug string, data []byte) error {
				r, err := parseRole(slug, data)
				if err != nil {
					return err
				}
				s.roles[r.Slug] = r
				s.sources["role/"+r.Slug] = "local"
				return nil
			})
		for _, mem := range ws.Members() {
			for _, sub := range memberSkillDirs {
				s.loadSkillDir(filepath.Join(mem.Path, sub), SourceMember)
			}
		}
		s.loadSkillDir(filepath.Join(ws.Root, workspace.DirSkills), SourceLocal)
	}
	return s
}

// memberSkillDirs are where a member repo keeps skills for other tools:
// the cross-tool convention and Claude Code's. A workspace's members are
// repos its owner added, so they are trusted (the client guide's rule).
var memberSkillDirs = []string{".agents/skills", ".claude/skills"}

// skillDirs lists every skill directory Open reads, for Fingerprint.
func skillDirs(ws *workspace.Workspace) []string {
	var out []string
	for _, d := range []string{AgentsSkillsDir, UserSkillsDir} {
		if d != "" {
			out = append(out, d)
		}
	}
	if ws != nil {
		for _, mem := range ws.Members() {
			for _, sub := range memberSkillDirs {
				out = append(out, filepath.Join(mem.Path, sub))
			}
		}
		out = append(out, filepath.Join(ws.Root, workspace.DirSkills))
	}
	return out
}

// loadBuiltinSkills reads the embedded skills; their directory is under
// BuiltinSkillsDir when the app wrote them out.
func (s *Store) loadBuiltinSkills() {
	entries, err := builtinFS.ReadDir("builtin/skills")
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("builtin skills unreadable: %v", err))
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := builtinFS.ReadFile(path.Join("builtin/skills", e.Name(), skills.File))
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("builtin skill %s: %v", e.Name(), err))
			continue
		}
		k, err := skills.Parse(e.Name(), data)
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("builtin skill %s: %v", e.Name(), err))
			continue
		}
		if BuiltinSkillsDir != "" {
			k.Dir = filepath.Join(BuiltinSkillsDir, e.Name())
		}
		s.putSkill(k, SourceBuiltin)
	}
}

// loadSkillDir reads every skill directory under dir.
func (s *Store) loadSkillDir(dir, source string) {
	if dir == "" {
		return
	}
	for _, f := range skills.Scan(dir) {
		if f.Err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("skill %s: %v (skipped)", f.Dir, f.Err))
			continue
		}
		s.putSkill(f.Skill, source)
	}
}

func (s *Store) putSkill(k *Skill, source string) {
	for _, w := range k.Warnings {
		s.warnings = append(s.warnings, fmt.Sprintf("skill %s: %s", k.Slug, w))
	}
	if prev, ok := s.sources["skill/"+k.Slug]; ok && prev != SourceBuiltin {
		s.warnings = append(s.warnings, fmt.Sprintf("skill %s (%s) is shadowed by the %s one", k.Slug, prev, source))
	}
	s.skills[k.Slug] = k
	s.sources["skill/"+k.Slug] = source
}

// MaterializeBuiltins writes the built-in skills under dir (normally a
// versioned cache directory), so agents read them like any other skill.
// Existing files are left alone.
func MaterializeBuiltins(dir string) error {
	entries, err := builtinFS.ReadDir("builtin/skills")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := builtinFS.ReadFile(path.Join("builtin/skills", e.Name(), skills.File))
		if err != nil {
			return err
		}
		target := filepath.Join(dir, e.Name(), skills.File)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := atomicWrite(target, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadBuiltinDir(dir, ext string, put func(slug string, data []byte) error) {
	entries, err := builtinFS.ReadDir(dir)
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("builtin library %s unreadable: %v", dir, err))
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ext)
		data, err := builtinFS.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("builtin %s: %v", e.Name(), err))
			continue
		}
		if err := put(slug, data); err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("builtin %s: %v", e.Name(), err))
		}
	}
}

func (s *Store) loadLocalDir(dir, ext, kind string, put func(slug string, data []byte) error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("library: read %s: %v", dir, err))
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ext)
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("%s %s: %v", kind, e.Name(), err))
			continue
		}
		if err := put(slug, data); err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("%s %s: %v", kind, e.Name(), err))
		}
	}
}

// Role resolves one role: local cards shadow builtins.
func (s *Store) Role(slug string) (*Role, bool) {
	r, ok := s.roles[slug]
	return r, ok
}

// Skill resolves one skill: local cards shadow builtins.
func (s *Store) Skill(slug string) (*Skill, bool) {
	k, ok := s.skills[slug]
	return k, ok
}

// Source names where a slug resolved from ("builtin"/"local"/"").
func (s *Store) Source(kind, slug string) string {
	return s.sources[kind+"/"+slug]
}

// Roles lists every role, sorted by slug.
func (s *Store) Roles() []Entry {
	out := make([]Entry, 0, len(s.roles))
	for slug, r := range s.roles {
		out = append(out, Entry{Slug: slug, Kind: "role", Source: s.Source("role", slug), Description: r.Description})
	}
	sortEntries(out)
	return out
}

// Skills lists every skill, sorted by slug.
func (s *Store) Skills() []Entry {
	out := make([]Entry, 0, len(s.skills))
	for slug, k := range s.skills {
		out = append(out, Entry{Slug: slug, Kind: "skill", Source: s.Source("skill", slug), Description: k.Description})
	}
	sortEntries(out)
	return out
}

func sortEntries(e []Entry) {
	sort.Slice(e, func(i, j int) bool { return e[i].Slug < e[j].Slug })
}

// Warnings names every malformed card (and builtin defects).
func (s *Store) Warnings() []string { return s.warnings }

// WriteRole validates-then-writes a local role card atomically.
func (s *Store) WriteRole(ws *workspace.Workspace, r *Role) error {
	if !validSlug(r.Slug) {
		return fmt.Errorf("library: role slug %q is not a slug (lowercase [a-z0-9._-])", r.Slug)
	}
	if strings.TrimSpace(r.Description) == "" {
		return fmt.Errorf("library: role description is required")
	}
	if r.Preset != "" {
		if _, err := PolicyPresetJSON(r.Preset); err != nil {
			return err
		}
	}
	var f roleFile
	f.Schema = 1
	f.Description = r.Description
	f.Tools = r.Tools
	f.Preset = r.Preset
	f.System = r.System
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(f); err != nil {
		return fmt.Errorf("library: encode role: %w", err)
	}
	// Round-trip: what we write is what we would accept.
	back, err := parseRole(r.Slug, []byte(buf.String()))
	if err != nil {
		return fmt.Errorf("library: role self-check: %w", err)
	}
	if back.Description != r.Description || back.Preset != r.Preset || back.System != r.System ||
		strings.Join(back.Tools, ",") != strings.Join(r.Tools, ",") {
		return fmt.Errorf("library: role round-trip mismatch")
	}
	dir := filepath.Join(ws.Root, workspace.DirRoles)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("library: mkdir roles: %w", err)
	}
	return atomicWrite(filepath.Join(dir, r.Slug+".toml"), buf.String())
}

// Script resolves a script inside an installed skill (scripts/<name>,
// or any path within the skill directory) to an absolute executable
// path. Built-in skills ship no scripts. The caller runs it directly (no
// shell).
func (s *Store) Script(slug, rel string) (string, error) {
	k, ok := s.skills[slug]
	if !ok {
		return "", fmt.Errorf("library: skill %q not found", slug)
	}
	if s.sources["skill/"+slug] == SourceBuiltin {
		return "", fmt.Errorf("library: skill %q is built in and has no scripts", slug)
	}
	return skills.Script(k.Dir, rel)
}

// WriteSkill writes a skill directory into this workspace's .dhi/skills.
func (s *Store) WriteSkill(ws *workspace.Workspace, k *Skill) error {
	return WriteSkillAt(filepath.Join(ws.Root, workspace.DirSkills), k)
}

// WriteSkillAt writes a skill directory (<dir>/<name>/SKILL.md) into a
// workspace's .dhi/skills or UserSkillsDir, strictly valid per the spec.
func WriteSkillAt(dir string, k *Skill) error {
	if err := skills.Write(dir, k); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// Delete removes a LOCAL card; builtin slugs refuse by name.
func (s *Store) Delete(ws *workspace.Workspace, kind, slug string) error {
	src := s.Source(kind, slug)
	if kind == "skill" {
		k := s.skills[slug]
		switch {
		case k == nil:
			return fmt.Errorf("library: skill %q not found", slug)
		case src == SourceYours || src == SourceLocal:
			// Yours: deleting it removes it from every workspace.
			if err := os.RemoveAll(k.Dir); err != nil {
				return fmt.Errorf("library: delete skill %s: %w", slug, err)
			}
			return nil
		default:
			return fmt.Errorf("library: skill %q is %s — remove it where it was installed", slug, orBuiltin(src))
		}
	}
	if src != SourceLocal {
		return fmt.Errorf("library: %s %q is %s — only local cards can be deleted", kind, slug, orBuiltin(src))
	}
	var path string
	switch kind {
	case "role":
		path = filepath.Join(ws.Root, workspace.DirRoles, slug+".toml")
	case "persona":
		path = filepath.Join(ws.Root, workspace.DirPersonas, slug+".toml")
	default:
		return fmt.Errorf("library: unknown kind %q", kind)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("library: delete %s %s: %w", kind, slug, err)
	}
	return nil
}

func orBuiltin(src string) string {
	if src == "" {
		return "unknown"
	}
	return src
}

// atomicWrite writes via temp+rename so readers never see a torn card.
func atomicWrite(path, data string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
