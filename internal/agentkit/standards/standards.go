// Package standards resolves DHI's layered coding instructions
// (F-003/F-008 follow-up): guidance injected into every agent turn,
// independent of how the turn was triggered. Layers, most specific
// last: compiled built-ins → workspace (.dhi/standards.toml
// [workspace]) → each team the agent belongs to → per-agent override
// ("extend" appends, "replace" discards upstream). Resolution reads the
// file fresh per call so edits apply without reloads.
package standards

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the standards schema this build understands.
const SchemaVersion = 1

// File is the standards document under the workspace root.
const File = ".dhi/standards.toml"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Builtins are the shipped defaults every rostered agent receives.
var Builtins = []string{
	"Run the project's tests before declaring work done",
	"Never force-push or rewrite shared history",
	"Keep changes minimal and focused on the requested task",
	"Prefer editing existing files over creating new ones",
}

// Modes for the per-agent layer.
const (
	ModeExtend  = "extend"
	ModeReplace = "replace"
)

type teamLayer struct {
	Extend []string `toml:"extend"`
}

type agentLayer struct {
	Mode    string   `toml:"mode"`
	Entries []string `toml:"entries"`
}

type doc struct {
	Schema    int                   `toml:"schema"`
	Workspace []string              `toml:"workspace"`
	Teams     map[string]teamLayer  `toml:"teams"`
	Agents    map[string]agentLayer `toml:"agents"`
}

// TeamLookup resolves an agent id to its team slugs (wired from org).
type TeamLookup func(agentID string) []string

// Resolve renders the effective instruction block for agentID: bulleted
// lines, ready to append to a system prompt. Built-ins always apply;
// an absent/invalid document degrades to them.
func Resolve(root, agentID string, teams TeamLookup) string {
	lines := append([]string(nil), Builtins...)
	if d, err := loadDoc(root); err == nil {
		lines = append(lines, d.Workspace...)
		slugs := make([]string, 0, len(d.Teams))
		for slug := range d.Teams {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		for _, slug := range slugs {
			if teams != nil && contains(teams(agentID), slug) {
				lines = append(lines, d.Teams[slug].Extend...)
			}
		}
		if al, ok := d.Agents[agentID]; ok {
			if strings.TrimSpace(al.Mode) == ModeReplace {
				lines = append([]string(nil), Builtins...)
				lines = append(lines, al.Entries...)
			} else {
				lines = append(lines, al.Entries...)
			}
		}
	}
	return Render(lines)
}

// Render formats instruction lines as the prompt block.
func Render(lines []string) string {
	var b strings.Builder
	b.WriteString("Standing instructions (apply to all coding work):")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			b.WriteString("\n- ")
			b.WriteString(l)
		}
	}
	return b.String()
}

func loadDoc(root string) (*doc, error) {
	data, err := os.ReadFile(filepath.Join(root, File))
	if err != nil {
		return nil, err
	}
	var d doc
	md, err := toml.Decode(string(data), &d)
	if err != nil {
		return nil, fmt.Errorf("standards: parse %s: %w", File, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("standards: unknown key(s): %s", strings.Join(keys, ", "))
	}
	if d.Schema != SchemaVersion {
		return nil, fmt.Errorf("standards: schema %d, want %d", d.Schema, SchemaVersion)
	}
	return &d, nil
}

// ---- write API (UI layer persists through these) ----

// Save writes the given layers atomically; nil maps clear sections.
// Slugs are validated so malformed docs can never be persisted.
func Save(root string, workspaceEntries []string, teams map[string][]string,
	agents map[string]AgentOverride) error {

	d := doc{Schema: SchemaVersion,
		Teams: map[string]teamLayer{}, Agents: map[string]agentLayer{}}
	d.Workspace = cleanLines(workspaceEntries)
	for slug, entries := range teams {
		if !nameRe.MatchString(slug) {
			return fmt.Errorf("standards: bad team name %q", slug)
		}
		d.Teams[slug] = teamLayer{Extend: cleanLines(entries)}
	}
	for id, ov := range agents {
		if !nameRe.MatchString(id) {
			return fmt.Errorf("standards: bad agent id %q", id)
		}
		mode := strings.TrimSpace(ov.Mode)
		if mode != ModeExtend && mode != ModeReplace {
			return fmt.Errorf("standards: agent %q mode must be extend|replace", id)
		}
		d.Agents[id] = agentLayer{Mode: mode, Entries: cleanLines(ov.Entries)}
	}

	path := filepath.Join(root, File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("standards: write: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".standards-*.toml")
	if err != nil {
		return fmt.Errorf("standards: write: %w", err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(d); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("standards: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("standards: write: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("standards: write: %w", err)
	}
	return nil
}

// AgentOverride is one per-agent layer for the write API.
type AgentOverride struct {
	Mode    string
	Entries []string
}

func cleanLines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---- inspection (doctor / UI) ----

// Snapshot summarizes the document layers without leaking internals.
type Snapshot struct {
	Workspace []string
	Teams     map[string][]string
	Agents    map[string]AgentOverride
}

// Inspect loads and validates the document for tooling. A missing file
// returns a zero snapshot and no error (built-ins-only is healthy).
func Inspect(root string) (*Snapshot, error) {
	d, err := loadDoc(root)
	if os.IsNotExist(err) {
		return &Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{
		Workspace: append([]string(nil), d.Workspace...),
		Teams:     map[string][]string{},
		Agents:    map[string]AgentOverride{},
	}
	for slug, t := range d.Teams {
		snap.Teams[slug] = append([]string(nil), t.Extend...)
	}
	for id, a := range d.Agents {
		snap.Agents[id] = AgentOverride(a)
	}
	return snap, nil
}
