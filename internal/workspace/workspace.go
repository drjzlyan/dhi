// Package workspace models DHI's multi-repo workspace: a root directory
// holding `.dhi/workspace.toml` (member registry), the member repos, and
// the VPath resolver that names files across all members as
// "<member>/<rel-path>". The `.dhi/` tree is reserved for agents, memory,
// knowledge, channels, and tasks (dir-schema reservation).
//
// A Workspace is safe for concurrent use: members are guarded by an
// internal RWMutex, mutations persist atomically before becoming visible,
// and Subscribe fans out change events for live surfaces (P1 re-
// resolution). Read the roster through Members/Member — never by reaching
// into internals.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

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
	Root string

	mu      sync.RWMutex
	members []Member // sorted by name; guarded by mu

	subs   map[int]chan Change
	subSeq int
}

// config is the on-disk TOML shape of .dhi/workspace.toml.
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
	// Iterate names in sorted order: member order must be deterministic
	// across runs (map iteration is randomized) for stable UI and VPath
	// reverse-mapping semantics.
	names := make([]string, 0, len(cfg.Members))
	for name := range cfg.Members {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := cfg.Members[name]
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
		ws.members = append(ws.members, Member{Name: name, Path: abs})
	}
	if len(ws.members) == 0 {
		return nil, fmt.Errorf("workspace: no members configured")
	}
	return ws, nil
}

// Members returns a snapshot of the roster sorted by name.
func (w *Workspace) Members() []Member {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Member, len(w.members))
	copy(out, w.members)
	return out
}

// Member returns the member with the given name.
func (w *Workspace) Member(name string) (Member, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, m := range w.members {
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

// ValidateName reports whether alias satisfies the member naming rule
// (lowercase [a-z0-9._-], starting alphanumeric).
func ValidateName(name string) error {
	if !validName(name) {
		return fmt.Errorf("bad member name %q (lowercase [a-z0-9._-], start alphanumeric)", name)
	}
	return nil
}

// Save persists the current roster to .dhi/workspace.toml atomically.
// Paths under the workspace root are written relative to it so configs
// stay portable; anything outside is stored absolute.
func (w *Workspace) Save() error {
	w.mu.RLock()
	snap := make([]Member, len(w.members))
	copy(snap, w.members)
	w.mu.RUnlock()
	return saveMembers(w.Root, snap)
}

// saveMembers serializes the given roster atomically (temp file in the
// target directory, then rename).
func saveMembers(root string, members []Member) error {
	cfg := config{Schema: SchemaVersion, Members: map[string]member{}}
	for _, m := range members {
		p := m.Path
		if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = rel
		}
		cfg.Members[m.Name] = member{Path: p}
	}

	cfgPath := filepath.Join(root, ConfigFile)
	tmp, err := os.CreateTemp(filepath.Dir(cfgPath), ".workspace-*.toml")
	if err != nil {
		return fmt.Errorf("workspace: write config: %w", err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("workspace: encode config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("workspace: write config: %w", err)
	}
	if err := os.Rename(name, cfgPath); err != nil {
		os.Remove(name)
		return fmt.Errorf("workspace: write config: %w", err)
	}
	return nil
}

// resolveMemberPath validates and absolutizes a candidate member path:
// relative inputs join the workspace root; the target must exist as a
// directory.
func (w *Workspace) resolveMemberPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is empty")
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(w.Root, abs)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("path %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %s is not a directory", abs)
	}
	return abs, nil
}

// AddMember registers name → path (relative paths resolve against the
// workspace root). The roster is persisted first; memory changes only on
// success. Duplicate names or paths are rejected.
func (w *Workspace) AddMember(name, path string) error {
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	abs, err := w.resolveMemberPath(path)
	if err != nil {
		return fmt.Errorf("workspace: add %q: %w", name, err)
	}

	w.mu.Lock()
	for _, m := range w.members {
		if m.Name == name {
			w.mu.Unlock()
			return fmt.Errorf("workspace: member %q already exists", name)
		}
		if m.Path == abs {
			w.mu.Unlock()
			return fmt.Errorf("workspace: path %s already registered as %q", abs, m.Name)
		}
	}
	next := make([]Member, len(w.members), len(w.members)+1)
	copy(next, w.members)
	next = append(next, Member{Name: name, Path: abs})
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
	w.mu.Unlock()

	if err := saveMembers(w.Root, next); err != nil {
		return err
	}
	w.mu.Lock()
	w.members = next
	w.mu.Unlock()
	w.notify(Change{Kind: Added, Name: name})
	return nil
}

// RemoveMember unregisters name without touching its working tree
// (deleting checkouts always requires explicit user confirmation at the
// UI layer). The last member cannot be removed — a workspace with zero
// members is not loadable, so the invariant holds everywhere.
func (w *Workspace) RemoveMember(name string) error {
	w.mu.Lock()
	idx := -1
	for i, m := range w.members {
		if m.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		w.mu.Unlock()
		return fmt.Errorf("workspace: unknown member %q", name)
	}
	if len(w.members) == 1 {
		w.mu.Unlock()
		return fmt.Errorf("workspace: cannot remove the last member %q", name)
	}
	next := make([]Member, 0, len(w.members)-1)
	next = append(next, w.members[:idx]...)
	next = append(next, w.members[idx+1:]...)
	w.mu.Unlock()

	if err := saveMembers(w.Root, next); err != nil {
		return err
	}
	w.mu.Lock()
	w.members = next
	w.mu.Unlock()
	w.notify(Change{Kind: Removed, Name: name})
	return nil
}

// RenameMember re-aliases old to new. VPaths recorded elsewhere (open
// buffers, tasks) keep resolving through the new alias after surfaces
// reload; history on disk is not rewritten.
func (w *Workspace) RenameMember(oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return fmt.Errorf("workspace: rename %q: %w", oldName, err)
	}
	w.mu.Lock()
	idx := -1
	for i, m := range w.members {
		if m.Name == oldName {
			idx = i
			break
		}
		if m.Name == newName {
			w.mu.Unlock()
			return fmt.Errorf("workspace: member %q already exists", newName)
		}
	}
	if idx < 0 {
		w.mu.Unlock()
		return fmt.Errorf("workspace: unknown member %q", oldName)
	}
	next := make([]Member, len(w.members))
	copy(next, w.members)
	next[idx].Name = newName
	sort.Slice(next, func(i, j int) bool { return next[i].Name < next[j].Name })
	w.mu.Unlock()

	if err := saveMembers(w.Root, next); err != nil {
		return err
	}
	w.mu.Lock()
	w.members = next
	w.mu.Unlock()
	w.notify(Change{Kind: Renamed, Name: newName, From: oldName, To: newName})
	return nil
}

// ChangeKind names a roster mutation.
type ChangeKind string

// Change kinds.
const (
	Added   ChangeKind = "added"
	Removed ChangeKind = "removed"
	Renamed ChangeKind = "renamed"
)

// Change announces one committed roster mutation.
type Change struct {
	Kind ChangeKind
	Name string // affected member (new name when renamed)
	From string // previous alias (renamed only)
	To   string // new alias (renamed only)
	Path string // member path (added only)
}

// Subscribe receives every subsequent roster change until cancel runs.
// Delivery is best-effort (a full buffer drops events; surfaces refresh
// from snapshots anyway).
func (w *Workspace) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 8)
	w.mu.Lock()
	w.subSeq++
	id := w.subSeq
	if w.subs == nil {
		w.subs = map[int]chan Change{}
	}
	w.subs[id] = ch
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		delete(w.subs, id)
		w.mu.Unlock()
	}
}

// notify fans out one change without blocking on slow subscribers.
func (w *Workspace) notify(c Change) {
	w.mu.Lock()
	targets := make([]chan Change, 0, len(w.subs))
	for _, sub := range w.subs {
		targets = append(targets, sub)
	}
	w.mu.Unlock()
	for _, sub := range targets {
		select {
		case sub <- c:
		default:
		}
	}
}
