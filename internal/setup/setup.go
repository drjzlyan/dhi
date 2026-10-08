// Package setup holds the non-UI logic of the first-run wizard (F-043):
// persisted progress, the auto-run policy, and workspace initialisation.
// It imports no tui package; the wizard surface injects it.
package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// StateFile is the progress file name (user scope under the config dir,
// workspace scope under .dhi/).
const StateFile = "setup.json"

// State records what the wizard has done. Finished is set when a run
// reaches its last step; Steps maps a step id to when it was applied.
type State struct {
	Finished bool              `json:"finished"`
	Steps    map[string]string `json:"steps,omitempty"`
}

// Mark records a step as applied.
func (s *State) Mark(id string, at time.Time) {
	if s.Steps == nil {
		s.Steps = map[string]string{}
	}
	s.Steps[id] = at.UTC().Format(time.RFC3339)
}

// Has reports whether a step was applied before.
func (s State) Has(id string) bool { _, ok := s.Steps[id]; return ok }

// LoadState reads a state file; a missing file is the zero State.
func LoadState(path string) (State, error) {
	var s State
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("setup: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("setup: parse %s: %w", path, err)
	}
	return s, nil
}

// SaveState writes the state atomically enough for a single-user file.
func SaveState(path string, s State) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("setup: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// UserStatePath is <config dir>/dhi/setup.json, next to config.toml.
func UserStatePath() (string, error) {
	cfg, err := settings.DefaultUserPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), StateFile), nil
}

// WorkspaceStatePath is <root>/.dhi/setup.json (gitignored).
func WorkspaceStatePath(root string) string {
	return filepath.Join(root, workspace.DHIDir, StateFile)
}

// AutoRunInput is what the launch policy looks at.
type AutoRunInput struct {
	HasWorkspace    bool
	UserFinished    bool // the user-scope wizard completed once before
	WorkspaceSetup  bool // .dhi/setup.json exists
	WelcomeSeen     bool // .dhi/welcome.seen exists (pre-wizard workspaces)
	ForcedByCommand bool // the palette asked for it
}

// ShouldAutoRun decides whether launch shows the wizard. It never pushes
// an established workspace through it: only a first-time user (nothing
// finished before) in either a bare directory or a workspace nobody has
// onboarded into. Everyone else reaches it from the palette.
func ShouldAutoRun(in AutoRunInput) bool {
	if in.ForcedByCommand {
		return true
	}
	if in.UserFinished {
		return false
	}
	if !in.HasWorkspace {
		return true
	}
	return !in.WorkspaceSetup && !in.WelcomeSeen
}

// MemberSpec is a proposed workspace member.
type MemberSpec struct {
	Name string
	Path string // relative to the workspace root; "." = the root itself
}

// DiscoverMembers proposes members for a new workspace at root: the root
// itself when it is a git repo, else each child directory that is one,
// else the root as a single member. Names are sanitised and de-duplicated.
func DiscoverMembers(root string) []MemberSpec {
	root = filepath.Clean(root)
	single := func() []MemberSpec {
		name := workspace.SanitizeName(filepath.Base(root))
		if name == "" {
			name = "workspace"
		}
		return []MemberSpec{{Name: name, Path: "."}}
	}
	if isGit(root) {
		return single()
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return single()
	}
	var out []MemberSpec
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || e.Name()[0] == '.' {
			continue
		}
		if !isGit(filepath.Join(root, e.Name())) {
			continue
		}
		name := workspace.SanitizeName(e.Name())
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, MemberSpec{Name: name, Path: e.Name()})
	}
	if len(out) == 0 {
		return single()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func isGit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// GitignoreBody keeps per-user runtime state out of version control while
// the contract (workspace.toml, conventions.toml, standards.toml, agents/)
// stays tracked. It is written to .dhi/.gitignore for new workspaces.
const GitignoreBody = `# DHI per-user runtime state (written by dhi setup). The contract files —
# workspace.toml, conventions.toml, standards.toml, agents/*.toml — stay tracked.
channels/
knowledge/
memory/
reviews/
sessions/
tasks/
agents/.archived/
agents/*/runs/
config.toml
marketplace.json
unread.json
welcome.seen
setup.json
org.toml
`

// InitWorkspace creates a workspace at root with the given members (or
// the discovered ones when none are given) and the .dhi/.gitignore. It
// refuses to overwrite an existing workspace.
func InitWorkspace(root string, members []MemberSpec) ([]MemberSpec, error) {
	if len(members) == 0 {
		members = DiscoverMembers(root)
	}
	m := map[string]string{}
	for _, mem := range members {
		m[mem.Name] = mem.Path
	}
	if err := workspace.CreateWith(root, m); err != nil {
		return nil, err
	}
	ign := filepath.Join(root, workspace.DHIDir, ".gitignore")
	if _, err := os.Stat(ign); os.IsNotExist(err) {
		if err := os.WriteFile(ign, []byte(GitignoreBody), 0o644); err != nil {
			return nil, fmt.Errorf("setup: write .dhi/.gitignore: %w", err)
		}
	}
	if _, err := workspace.Load(root); err != nil {
		return nil, fmt.Errorf("setup: created workspace does not load: %w", err)
	}
	return members, nil
}
