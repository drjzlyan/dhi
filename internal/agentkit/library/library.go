// Package library is DHI's behaviour library (F-027, ADR-0016): the
// predefined format for agent people. Roles are job descriptions
// (default tools, policy preset, system template); skills are
// teachable instruction documents. The built-in set ships embedded in
// the binary; user-authored cards under .dhi/ shadow built-ins by
// slug; packs may ship both (F-019). Malformed cards become named
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

// Skill is one teachable instruction document. An optional Script names
// an executable file (relative to .dhi/skills, no `..`) the skill can
// run — the human runs it from LIBRARY; agents request it via the served
// `skill_run` tool. Scripts execute directly (shebang, no shell) under
// the OS sandbox with network denied unless the exec/network scope says
// otherwise.
type Skill struct {
	Slug        string
	Name        string
	Description string
	Script      string // relative to .dhi/skills; "" = instruction-only
	Body        string // markdown instruction body
}

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

// parseSkill splits one skill markdown doc into frontmatter + body.
// Frontmatter is `key: value` lines (name, description, optional
// script) inside a --- fence; the body is the rest.
func parseSkill(slug string, data []byte) (*Skill, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("frontmatter fence missing (start the file with ---)")
	}
	name, desc, script := "", "", ""
	i := 1
	for ; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "---" {
			i++
			break
		}
		switch {
		case strings.HasPrefix(l, "name:"):
			name = strings.TrimSpace(strings.TrimPrefix(l, "name:"))
		case strings.HasPrefix(l, "description:"):
			desc = strings.TrimSpace(strings.TrimPrefix(l, "description:"))
		case strings.HasPrefix(l, "script:"):
			script = strings.TrimSpace(strings.TrimPrefix(l, "script:"))
		default:
			return nil, fmt.Errorf("frontmatter line %d: %q (want name:, description:, or script:)", i+1, l)
		}
	}
	if name == "" {
		return nil, fmt.Errorf("frontmatter name is required")
	}
	if desc == "" {
		return nil, fmt.Errorf("frontmatter description is required")
	}
	if script != "" {
		if err := validScriptRel(script); err != nil {
			return nil, err
		}
	}
	body := strings.TrimSpace(strings.Join(lines[i:], "\n"))
	if body == "" {
		return nil, fmt.Errorf("skill body is empty")
	}
	return &Skill{Slug: slug, Name: name, Description: desc, Script: script, Body: body}, nil
}

// validScriptRel enforces that a skill's script path is relative and
// stays inside the skills dir (no absolute paths, no `..`).
func validScriptRel(s string) error {
	if filepath.IsAbs(s) || strings.ContainsAny(s, "\\:") {
		return fmt.Errorf("script %q must be a relative path", s)
	}
	c := path.Clean(s)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return fmt.Errorf("script %q escapes the skills directory", s)
	}
	return nil
}

// ParseRole validates one role card's bytes without writing it (packs
// pre-validate every card before the first file lands, ADR-0022).
func ParseRole(slug string, data []byte) (*Role, error) { return parseRole(slug, data) }

// ParseSkill validates one skill doc's bytes without writing it.
func ParseSkill(slug string, data []byte) (*Skill, error) { return parseSkill(slug, data) }

// WriteRole is the package-level writer (the Store method needs no
// state); packs call it after ParseRole.
func WriteRole(ws *workspace.Workspace, r *Role) error {
	var s Store
	return s.WriteRole(ws, r)
}

// WriteSkill is the package-level writer for skill docs.
func WriteSkill(ws *workspace.Workspace, k *Skill) error {
	var s Store
	return s.WriteSkill(ws, k)
}

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
	for _, dir := range []string{workspace.DirRoles, workspace.DirSkills, workspace.DirPersonas} {
		entries, err := os.ReadDir(filepath.Join(ws.Root, dir))
		if err != nil {
			continue
		}
		b.WriteString(dir + ":")
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				fmt.Fprintf(&b, "%s|%d|%d;", e.Name(), info.Size(), info.ModTime().UnixNano())
			}
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
	s.loadBuiltinDir("builtin/skills", ".md", func(slug string, data []byte) error {
		k, err := parseSkill(slug, data)
		if err != nil {
			return err
		}
		s.skills[k.Slug] = k
		s.sources["skill/"+k.Slug] = "builtin"
		return nil
	})
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
		s.loadLocalDir(filepath.Join(ws.Root, workspace.DirSkills), ".md", "skill",
			func(slug string, data []byte) error {
				k, err := parseSkill(slug, data)
				if err != nil {
					return err
				}
				s.skills[k.Slug] = k
				s.sources["skill/"+k.Slug] = "local"
				return nil
			})
	}
	return s
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

// Script resolves a local skill's declared script to an absolute path,
// refusing builtin/absent scripts, non-executable files, and any path
// that escapes .dhi/skills. The caller runs it directly (no shell).
func (s *Store) Script(slug string) (string, error) {
	k, ok := s.skills[slug]
	if !ok {
		return "", fmt.Errorf("library: skill %q not found", slug)
	}
	if s.sources["skill/"+slug] != "local" {
		return "", fmt.Errorf("library: skill %q is %s — scripts run only from local cards", slug, orBuiltin(s.sources["skill/"+slug]))
	}
	if s.root == "" {
		return "", fmt.Errorf("library: no workspace root — scripts unavailable")
	}
	if strings.TrimSpace(k.Script) == "" {
		return "", fmt.Errorf("library: skill %q declares no script", slug)
	}
	if err := validScriptRel(k.Script); err != nil {
		return "", err
	}
	dir := filepath.Join(s.root, workspace.DirSkills)
	abs := filepath.Join(dir, filepath.FromSlash(path.Clean(k.Script)))
	if rel, err := filepath.Rel(dir, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("library: skill %q script escapes the skills directory", slug)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("library: skill %q script %s: %w", slug, k.Script, err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("library: skill %q script %s is a directory", slug, k.Script)
	}
	if fi.Mode()&0o111 == 0 {
		return "", fmt.Errorf("library: skill %q script %s is not executable (chmod +x)", slug, k.Script)
	}
	return abs, nil
}

// WriteSkill validates-then-writes a local skill doc atomically.
func (s *Store) WriteSkill(ws *workspace.Workspace, k *Skill) error {
	if !validSlug(k.Slug) {
		return fmt.Errorf("library: skill slug %q is not a slug (lowercase [a-z0-9._-])", k.Slug)
	}
	if strings.TrimSpace(k.Name) == "" {
		return fmt.Errorf("library: skill name is required")
	}
	if strings.TrimSpace(k.Description) == "" {
		return fmt.Errorf("library: skill description is required")
	}
	if strings.TrimSpace(k.Body) == "" {
		return fmt.Errorf("library: skill body is empty")
	}
	if strings.TrimSpace(k.Script) != "" {
		if err := validScriptRel(k.Script); err != nil {
			return err
		}
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + k.Name + "\n")
	b.WriteString("description: " + k.Description + "\n")
	if strings.TrimSpace(k.Script) != "" {
		b.WriteString("script: " + strings.TrimSpace(k.Script) + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(k.Body + "\n")
	back, err := parseSkill(k.Slug, []byte(b.String()))
	if err != nil {
		return fmt.Errorf("library: skill self-check: %w", err)
	}
	if back.Name != k.Name || back.Description != k.Description || back.Body != k.Body || back.Script != strings.TrimSpace(k.Script) {
		return fmt.Errorf("library: skill round-trip mismatch")
	}
	dir := filepath.Join(ws.Root, workspace.DirSkills)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("library: mkdir skills: %w", err)
	}
	return atomicWrite(filepath.Join(dir, k.Slug+".md"), b.String())
}

// Delete removes a LOCAL card; builtin slugs refuse by name.
func (s *Store) Delete(ws *workspace.Workspace, kind, slug string) error {
	if src := s.Source(kind, slug); src != "local" {
		return fmt.Errorf("library: %s %q is %s — only local cards can be deleted", kind, slug, orBuiltin(src))
	}
	var path string
	switch kind {
	case "role":
		path = filepath.Join(ws.Root, workspace.DirRoles, slug+".toml")
	case "persona":
		path = filepath.Join(ws.Root, workspace.DirPersonas, slug+".toml")
	default:
		path = filepath.Join(ws.Root, workspace.DirSkills, slug+".md")
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
