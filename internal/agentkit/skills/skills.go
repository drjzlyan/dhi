// Package skills reads and writes Agent Skills (agentskills.io, ADR-0043):
// a skill is a directory holding SKILL.md — YAML frontmatter (name,
// description, and the optional license, compatibility, metadata and
// allowed-tools) followed by Markdown instructions — plus any scripts/,
// references/ or assets/ it needs. It is the format Claude Code, Codex,
// Cursor, Copilot, opencode and the vendors' own plugins use, so a skill
// written for any of them works in DHI and the other way round.
//
// Reading is lenient, as the agentskills.io client guide recommends for
// skills authored for other clients: cosmetic problems are warnings and
// the skill still loads; only a missing description or unparseable YAML
// skips it. Writing is strict: what DHI writes passes the spec.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// File is the skill document's name inside its directory.
const File = "SKILL.md"

// Spec limits (agentskills.io/specification).
const (
	MaxName          = 64
	MaxDescription   = 1024
	MaxCompatibility = 500
)

// Skill is one Agent Skill.
type Skill struct {
	Slug          string // the directory name; how agents and manifests refer to it
	Name          string // frontmatter name (normally equal to Slug)
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  string // space-separated, experimental in the spec
	Body          string // Markdown after the frontmatter
	Dir           string // absolute skill directory ("" for an unmaterialized builtin)
	Warnings      []string
}

// Location is the absolute path of SKILL.md ("" without a directory).
func (s Skill) Location() string {
	if s.Dir == "" {
		return ""
	}
	return filepath.Join(s.Dir, File)
}

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName reports whether name meets the spec: 1–64 lowercase letters,
// digits and single hyphens, not at either end.
func ValidName(name string) bool { return len(name) <= MaxName && nameRe.MatchString(name) }

// Parse reads SKILL.md content for the skill in directory dirName.
func Parse(dirName string, data []byte) (*Skill, error) {
	text := strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\ufeff")
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("SKILL.md must start with --- frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("SKILL.md frontmatter has no closing ---")
	}
	front := rest[:end]
	body := rest[end+len("\n---"):]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	fm, err := decodeFront(front)
	if err != nil {
		return nil, err
	}
	s := &Skill{Slug: dirName, Body: strings.TrimSpace(body)}
	for k, v := range fm {
		switch k {
		case "name":
			s.Name = scalar(v)
		case "description":
			s.Description = strings.TrimSpace(scalar(v))
		case "license":
			s.License = scalar(v)
		case "compatibility":
			s.Compatibility = scalar(v)
		case "allowed-tools":
			s.AllowedTools = scalar(v)
		case "metadata":
			if m, ok := v.(map[string]any); ok {
				s.Metadata = map[string]string{}
				for mk, mv := range m {
					s.Metadata[mk] = scalar(mv)
				}
			}
		default:
			// Vendor extensions (disable-model-invocation, version, …)
			// are allowed; they do not change what DHI does.
		}
	}
	if s.Description == "" {
		return nil, fmt.Errorf("description is required (agents choose a skill by it)")
	}
	if s.Name == "" {
		s.Name = dirName
		s.Warnings = append(s.Warnings, "name is missing; using the directory name")
	}
	if s.Name != dirName && dirName != "" {
		s.Warnings = append(s.Warnings, fmt.Sprintf("name %q differs from its directory %q", s.Name, dirName))
	}
	if !ValidName(s.Name) {
		s.Warnings = append(s.Warnings, fmt.Sprintf("name %q is not a spec name (lowercase letters, digits, hyphens; at most %d)", s.Name, MaxName))
	}
	if len(s.Description) > MaxDescription {
		s.Warnings = append(s.Warnings, fmt.Sprintf("description is %d characters; the spec allows %d", len(s.Description), MaxDescription))
	}
	if len(s.Compatibility) > MaxCompatibility {
		s.Warnings = append(s.Warnings, fmt.Sprintf("compatibility is %d characters; the spec allows %d", len(s.Compatibility), MaxCompatibility))
	}
	return s, nil
}

// decodeFront parses the YAML frontmatter. Skills written for other
// clients often carry an unquoted value with a colon in it
// ("description: Use when: …"), which strict YAML refuses; those lines
// are quoted and parsed again, as the client guide suggests.
func decodeFront(front string) (map[string]any, error) {
	var fm map[string]any
	err := yaml.Unmarshal([]byte(front), &fm)
	if err == nil {
		return fm, nil
	}
	var fixed []string
	for _, l := range strings.Split(front, "\n") {
		k, v, ok := strings.Cut(l, ":")
		v = strings.TrimSpace(v)
		if ok && !strings.HasPrefix(l, " ") && strings.Contains(v, ": ") && !strings.HasPrefix(v, `"`) && !strings.HasPrefix(v, "'") {
			l = k + ": " + strconvQuote(v)
		}
		fixed = append(fixed, l)
	}
	if err2 := yaml.Unmarshal([]byte(strings.Join(fixed, "\n")), &fm); err2 != nil {
		return nil, fmt.Errorf("frontmatter is not valid YAML: %v", err)
	}
	return fm, nil
}

func strconvQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

// ReadDir loads the skill in dir (which must hold SKILL.md).
func ReadDir(dir string) (*Skill, error) {
	data, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	s, err := Parse(filepath.Base(abs), data)
	if err != nil {
		return nil, err
	}
	s.Dir = abs
	return s, nil
}

// Found is one skill a scan found, or why one could not load.
type Found struct {
	Skill *Skill
	Dir   string
	Err   error
}

// Scan finds every skill directory directly under root (root/<name>/
// SKILL.md). A missing root is empty. Hidden directories and
// node_modules are skipped.
func Scan(root string) []Found {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Found
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if strings.HasPrefix(n, ".") || n == "node_modules" {
			continue
		}
		dir := filepath.Join(root, n)
		if _, err := os.Stat(filepath.Join(dir, File)); err != nil {
			continue
		}
		s, err := ReadDir(dir)
		out = append(out, Found{Skill: s, Dir: dir, Err: err})
	}
	return out
}

// FindAll walks root (a cloned repository, a plugin) for skill
// directories at any depth up to maxDepth, for install sources that hold
// several skills (skills/<name>/SKILL.md, a plugin's skills dir).
func FindAll(root string, maxDepth int) []string {
	var dirs []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth || len(dirs) > 500 {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, File)); err == nil {
			dirs = append(dirs, dir)
			return // a skill's own subfolders are its resources
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			n := e.Name()
			if !e.IsDir() || strings.HasPrefix(n, ".") || n == "node_modules" {
				continue
			}
			walk(filepath.Join(dir, n), depth+1)
		}
	}
	walk(root, 0)
	sort.Strings(dirs)
	return dirs
}

// Check is the strict validation DHI applies to what it writes.
func Check(s *Skill) error {
	switch {
	case !ValidName(s.Name):
		return fmt.Errorf("name %q must be 1–%d lowercase letters, digits and single hyphens", s.Name, MaxName)
	case s.Slug != "" && s.Slug != s.Name:
		return fmt.Errorf("name %q must match its directory %q", s.Name, s.Slug)
	case strings.TrimSpace(s.Description) == "":
		return fmt.Errorf("description is required")
	case len(s.Description) > MaxDescription:
		return fmt.Errorf("description is %d characters; the limit is %d", len(s.Description), MaxDescription)
	case len(s.Compatibility) > MaxCompatibility:
		return fmt.Errorf("compatibility is %d characters; the limit is %d", len(s.Compatibility), MaxCompatibility)
	case strings.TrimSpace(s.Body) == "":
		return fmt.Errorf("the instructions are empty")
	}
	return nil
}

// Render is the SKILL.md text for s.
func Render(s *Skill) (string, error) {
	front := map[string]any{"name": s.Name, "description": s.Description}
	if s.License != "" {
		front["license"] = s.License
	}
	if s.Compatibility != "" {
		front["compatibility"] = s.Compatibility
	}
	if len(s.Metadata) > 0 {
		front["metadata"] = s.Metadata
	}
	if s.AllowedTools != "" {
		front["allowed-tools"] = s.AllowedTools
	}
	// name and description first, then the rest in a fixed order.
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range []string{"name", "description", "license", "compatibility", "allowed-tools", "metadata"} {
		v, ok := front[k]
		if !ok {
			continue
		}
		out, err := yaml.Marshal(map[string]any{k: v})
		if err != nil {
			return "", err
		}
		b.Write(out)
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(s.Body) + "\n")
	return b.String(), nil
}

// Write creates parent/<name>/SKILL.md for s (strictly valid), keeping
// any other files already in the directory.
func Write(parent string, s *Skill) error {
	if s.Slug == "" {
		s.Slug = s.Name
	}
	if err := Check(s); err != nil {
		return err
	}
	text, err := Render(s)
	if err != nil {
		return err
	}
	back, err := Parse(s.Slug, []byte(text))
	if err != nil || back.Description != strings.TrimSpace(s.Description) || back.Body != strings.TrimSpace(s.Body) {
		return fmt.Errorf("skill self-check failed: %v", err)
	}
	dir := filepath.Join(parent, s.Slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+File+".tmp")
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, File)); err != nil {
		return err
	}
	s.Dir = dir
	return nil
}

// Resources lists a skill's bundled files (relative paths, SKILL.md
// excluded), capped at limit; more reports that the list was cut.
func Resources(dir string, limit int) (files []string, more bool) {
	if dir == "" {
		return nil, false
	}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == File || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if len(files) >= limit {
			more = true
			return filepath.SkipAll
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, more
}

// Script resolves scripts/<name> (or any relative path inside the skill)
// to an executable file within dir, refusing escapes and non-executables.
func Script(dir, rel string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("this skill has no files on disk")
	}
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("script must be a path inside the skill, like scripts/run.sh")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if !strings.Contains(clean, string(filepath.Separator)) {
		clean = filepath.Join("scripts", clean) // "run.sh" means scripts/run.sh
	}
	abs := filepath.Join(dir, clean)
	if r, err := filepath.Rel(dir, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("script %q escapes the skill directory", rel)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("script %s: %w", rel, err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("script %s is a directory", rel)
	}
	if fi.Mode()&0o111 == 0 {
		return "", fmt.Errorf("script %s is not executable (chmod +x)", rel)
	}
	return abs, nil
}
