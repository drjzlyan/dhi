package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// Verbosity levels a persona may declare.
const (
	VerbosityTerse     = "terse"
	VerbosityBalanced  = "balanced"
	VerbosityThorough  = "thorough"
	maxPersonaTraits   = 8
	maxPersonaGuidance = 2000
)

// Persona is a communication style: how an employee speaks, independent
// of the job a Role describes (F-045). Rendering is deterministic so the
// same card always produces the same prompt block.
type Persona struct {
	Slug        string
	Description string
	Tone        string
	Verbosity   string   // terse | balanced | thorough
	Traits      []string // short behavioural statements
	Guidance    string   // free text appended verbatim
}

type personaFile struct {
	Schema      int      `toml:"schema"`
	Description string   `toml:"description"`
	Tone        string   `toml:"tone"`
	Verbosity   string   `toml:"verbosity"`
	Traits      []string `toml:"traits"`
	Guidance    string   `toml:"guidance"`
}

func parsePersona(slug string, data []byte) (*Persona, error) {
	var f personaFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, err
	}
	if und := md.Undecoded(); len(und) > 0 {
		var keys []string
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
	}
	if f.Schema != 1 {
		return nil, fmt.Errorf("schema %d, want 1", f.Schema)
	}
	p := &Persona{
		Slug:        slug,
		Description: strings.TrimSpace(f.Description),
		Tone:        strings.TrimSpace(f.Tone),
		Verbosity:   strings.TrimSpace(f.Verbosity),
		Guidance:    strings.TrimSpace(f.Guidance),
	}
	if p.Description == "" {
		return nil, fmt.Errorf("description is required")
	}
	switch p.Verbosity {
	case "", VerbosityTerse, VerbosityBalanced, VerbosityThorough:
	default:
		return nil, fmt.Errorf("verbosity %q (want %s, %s or %s)", p.Verbosity,
			VerbosityTerse, VerbosityBalanced, VerbosityThorough)
	}
	if len(f.Traits) > maxPersonaTraits {
		return nil, fmt.Errorf("%d traits (max %d)", len(f.Traits), maxPersonaTraits)
	}
	for _, t := range f.Traits {
		if t = strings.TrimSpace(t); t != "" {
			p.Traits = append(p.Traits, t)
		}
	}
	if len(p.Guidance) > maxPersonaGuidance {
		return nil, fmt.Errorf("guidance is %d chars (max %d)", len(p.Guidance), maxPersonaGuidance)
	}
	return p, nil
}

// ParsePersona validates one persona card's bytes without writing it.
func ParsePersona(slug string, data []byte) (*Persona, error) { return parsePersona(slug, data) }

var verbositySentence = map[string]string{
	VerbosityTerse:    "Keep replies as short as the task allows.",
	VerbosityBalanced: "Be as long as the answer needs and no longer.",
	VerbosityThorough: "Explain your reasoning and show your work.",
}

// Render is the prompt block this persona contributes: a "Voice" section.
// A persona with nothing to say renders "" so callers add no stray block.
func (p *Persona) Render() string {
	if p == nil {
		return ""
	}
	var parts []string
	if p.Tone != "" {
		parts = append(parts, "Voice: "+p.Tone+".")
	}
	if s, ok := verbositySentence[p.Verbosity]; ok {
		parts = append(parts, s)
	}
	head := strings.Join(parts, " ")
	var b strings.Builder
	b.WriteString(head)
	for _, t := range p.Traits {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- " + t)
	}
	if p.Guidance != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p.Guidance)
	}
	return b.String()
}

// Persona resolves one persona: local cards shadow builtins.
func (s *Store) Persona(slug string) (*Persona, bool) {
	p, ok := s.personas[slug]
	return p, ok
}

// Personas lists every persona, sorted by slug.
func (s *Store) Personas() []Entry {
	out := make([]Entry, 0, len(s.personas))
	for slug, p := range s.personas {
		out = append(out, Entry{Slug: slug, Kind: "persona", Source: s.Source("persona", slug), Description: p.Description})
	}
	sortEntries(out)
	return out
}

// loadPersonas fills the store from builtins then the local directory.
func (s *Store) loadPersonas(ws *workspace.Workspace) {
	s.loadBuiltinDir("builtin/personas", ".toml", func(slug string, data []byte) error {
		p, err := parsePersona(slug, data)
		if err != nil {
			return err
		}
		s.personas[p.Slug] = p
		s.sources["persona/"+p.Slug] = "builtin"
		return nil
	})
	if ws == nil {
		return
	}
	s.loadLocalDir(filepath.Join(ws.Root, workspace.DirPersonas), ".toml", "persona",
		func(slug string, data []byte) error {
			p, err := parsePersona(slug, data)
			if err != nil {
				return err
			}
			s.personas[p.Slug] = p
			s.sources["persona/"+p.Slug] = "local"
			return nil
		})
}

// WritePersona validates-then-writes a local persona card atomically.
func (s *Store) WritePersona(ws *workspace.Workspace, p *Persona) error {
	if !validSlug(p.Slug) {
		return fmt.Errorf("library: persona slug %q is not a slug (lowercase [a-z0-9._-])", p.Slug)
	}
	f := personaFile{Schema: 1, Description: p.Description, Tone: p.Tone,
		Verbosity: p.Verbosity, Traits: p.Traits, Guidance: p.Guidance}
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(f); err != nil {
		return fmt.Errorf("library: encode persona: %w", err)
	}
	if _, err := parsePersona(p.Slug, []byte(buf.String())); err != nil {
		return fmt.Errorf("library: persona self-check: %w", err)
	}
	dir := filepath.Join(ws.Root, workspace.DirPersonas)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("library: mkdir personas: %w", err)
	}
	return atomicWrite(filepath.Join(dir, p.Slug+".toml"), buf.String())
}
