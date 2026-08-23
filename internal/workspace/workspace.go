// Package workspace models DHI's multi-repo workspace: a root directory
// holding `.dhi/workspace.toml` (member registry), the member repos, and
// the VPath resolver that names files across all members as
// "<member>/<rel-path>". The `.dhi/` tree is reserved for agents, memory,
// knowledge, channels, and tasks (dir-schema reservation).
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the workspace.toml schema this build understands.
const SchemaVersion = 1

// Reserved paths inside a workspace root.
const (
	DHIDir       = ".dhi"
	ConfigFile   = ".dhi/workspace.toml"
	DirAgents    = ".dhi/agents"
	DirMemory    = ".dhi/memory"
	DirKnowledge = ".dhi/knowledge"
	DirChannels  = ".dhi/channels"
	DirTasks     = ".dhi/tasks"
)

// Member is one repo registered in the workspace.
type Member struct {
	Name string // alias used in vpaths; lowercase
	Path string // absolute path to the repo root
}

// Workspace is the loaded multi-repo model.
type Workspace struct {
	Root    string
	Members []Member
}

// Config is the on-disk TOML shape of .dhi/workspace.toml.
type config struct {
	Schema  int               `toml:"schema"`
	Members map[string]member `toml:"members"`
}

type member struct {
	Path string `toml:"path"`
}

// Create writes an initial workspace.toml under root and reserves the
// `.dhi/` schema directories. Existing configs are never overwritten —
// call Load instead.
func Create(root string, names ...string) error {
	cfgPath := filepath.Join(root, ConfigFile)
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("workspace: %s already exists", cfgPath)
	}
	for _, dir := range []string{DirAgents, DirMemory, DirKnowledge, DirChannels, DirTasks} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return fmt.Errorf("workspace: reserve %s: %w", dir, err)
		}
	}
	cfg := config{Schema: SchemaVersion, Members: map[string]member{}}
	for _, name := range names {
		cfg.Members[name] = member{Path: name}
	}
	f, err := os.Create(cfgPath)
	if err != nil {
		return fmt.Errorf("workspace: write config: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("workspace: encode config: %w", err)
	}
	return nil
}

// Load parses and validates <root>/.dhi/workspace.toml. Member paths may
// be relative to the workspace root; they are resolved to absolute paths
// and must exist on disk. Duplicate registrations (by name or by
// resolved path) are rejected.
func Load(root string) (*Workspace, error) {
	root = filepath.Clean(root)
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("workspace: %s not found under %s (not a DHI workspace?)", ConfigFile, root)
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: read config: %w", err)
	}
	var cfg config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("workspace: parse %s: %w", ConfigFile, err)
	}
	if cfg.Schema != SchemaVersion {
		return nil, fmt.Errorf("workspace: schema %d, want %d", cfg.Schema, SchemaVersion)
	}

	ws := &Workspace{Root: root}
	seenPaths := map[string]bool{}
	for name, m := range cfg.Members {
		if !validName(name) {
			return nil, fmt.Errorf("workspace: bad member name %q (lowercase [a-z0-9._-])", name)
		}
		if m.Path == "" {
			return nil, fmt.Errorf("workspace: member %q has no path", name)
		}
		abs := m.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		abs = filepath.Clean(abs)
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("workspace: member %q path %s: %w", name, abs, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("workspace: member %q path %s is not a directory", name, abs)
		}
		if seenPaths[abs] {
			return nil, fmt.Errorf("workspace: duplicate member path %s", abs)
		}
		seenPaths[abs] = true
		ws.Members = append(ws.Members, Member{Name: name, Path: abs})
	}
	if len(ws.Members) == 0 {
		return nil, fmt.Errorf("workspace: no members configured")
	}
	return ws, nil
}

// Member returns the member with the given name.
func (w *Workspace) Member(name string) (Member, bool) {
	for _, m := range w.Members {
		if m.Name == name {
			return m, true
		}
	}
	return Member{}, false
}

var memberNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func validName(name string) bool {
	return memberNameRe.MatchString(name)
}
