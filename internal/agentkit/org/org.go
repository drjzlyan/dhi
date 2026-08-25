// Package org is DHI's company registry: teams, leads, and membership
// live in `.dhi/org.toml` — a sidecar that never touches per-agent
// manifests (which stay strictly single-agent, schema v1). The human is
// addressable as "you". Like workspace rosters, mutations persist
// atomically before becoming visible, and Subscribe fans out changes.
package org

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the org.toml schema this build understands.
const SchemaVersion = 1

// Human is the team-member alias for the user; leads may be "you" or an
// agent id.
const Human = "you"

// File is the org document path under the workspace root.
const File = ".dhi/org.toml"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Team is one organizational unit.
type Team struct {
	Name    string   // slug
	Lead    string   // member id ("you" or agent id); "" = none yet
	Members []string // sorted agent ids
}

func (t Team) clone() Team {
	out := t
	out.Members = append([]string(nil), t.Members...)
	return out
}

// file is the on-disk TOML shape.
type file struct {
	Schema int             `toml:"schema"`
	Teams  map[string]team `toml:"teams"`
}

type team struct {
	Lead    string   `toml:"lead"`
	Members []string `toml:"members"`
}

// Org is the loaded company registry; safe for concurrent use.
type Org struct {
	root string

	mu    sync.RWMutex
	teams map[string]Team // slug → team

	subs   map[int]chan Change
	subSeq int
}

// Change announces one committed org mutation.
type Change struct {
	Kind ChangeKind
	Team string
}

// ChangeKind names a mutation.
type ChangeKind string

// Change kinds.
const (
	TeamAdded   ChangeKind = "team_added"
	TeamUpdated ChangeKind = "team_updated"
	TeamRemoved ChangeKind = "team_removed"
)

// Load reads <root>/.dhi/org.toml. A missing file is a valid empty org;
// malformed files error loudly.
func Load(root string) (*Org, error) {
	o := &Org{root: filepath.Clean(root), teams: map[string]Team{}}
	data, err := os.ReadFile(filepath.Join(root, File))
	if os.IsNotExist(err) {
		return o, nil
	}
	if err != nil {
		return nil, fmt.Errorf("org: read %s: %w", File, err)
	}
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, fmt.Errorf("org: parse %s: %w", File, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, 0, len(und))
		for _, k := range und {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("org: %s: unknown key(s): %s", File, strings.Join(keys, ", "))
	}
	if f.Schema != SchemaVersion {
		return nil, fmt.Errorf("org: schema %d, want %d", f.Schema, SchemaVersion)
	}
	for slug, t := range f.Teams {
		tm := Team{Name: slug, Lead: strings.TrimSpace(t.Lead)}
		seen := map[string]bool{}
		for _, m := range t.Members {
			m = strings.TrimSpace(m)
			if m == "" || seen[m] {
				continue
			}
			seen[m] = true
			tm.Members = append(tm.Members, m)
		}
		sort.Strings(tm.Members)
		o.teams[slug] = tm
	}
	return o, nil
}

// Teams returns every team sorted by name.
func (o *Org) Teams() []Team {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]Team, 0, len(o.teams))
	for _, t := range o.teams {
		out = append(out, t.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Team returns one team by slug.
func (o *Org) Team(slug string) (Team, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	t, ok := o.teams[slug]
	if !ok {
		return Team{}, false
	}
	return t.clone(), true
}

// TeamsOf lists the slugs of every team containing agentID, sorted.
func (o *Org) TeamsOf(agentID string) []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	var out []string
	for slug, t := range o.teams {
		for _, m := range t.Members {
			if m == agentID {
				out = append(out, slug)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// ValidMemberName applies the shared slug rule to team names and member
// ids alike. The human alias is always legal.
func ValidMemberName(s string) bool {
	return s == Human || nameRe.MatchString(s)
}

// CreateTeam adds a team; lead may be "" (none yet), Human, or an agent
// id. Duplicate slugs are rejected.
func (o *Org) CreateTeam(slug, lead string, members []string) error {
	if !nameRe.MatchString(slug) {
		return fmt.Errorf("org: bad team name %q (lowercase [a-z0-9._-])", slug)
	}
	tm, err := normalizeTeam(lead, members)
	if err != nil {
		return err
	}
	tm.Name = slug

	o.mu.Lock()
	if _, exists := o.teams[slug]; exists {
		o.mu.Unlock()
		return fmt.Errorf("org: team %q already exists", slug)
	}
	next := o.snapshotTeams()
	next[slug] = tm
	o.mu.Unlock()

	if err := o.save(next); err != nil {
		return err
	}
	o.commit(func(teams map[string]Team) { teams[slug] = tm },
		Change{Kind: TeamAdded, Team: slug})
	return nil
}

// UpdateTeam replaces lead/membership of an existing team atomically.
func (o *Org) UpdateTeam(slug, lead string, members []string) error {
	tm, err := normalizeTeam(lead, members)
	if err != nil {
		return err
	}
	tm.Name = slug

	o.mu.Lock()
	if _, exists := o.teams[slug]; !exists {
		o.mu.Unlock()
		return fmt.Errorf("org: unknown team %q", slug)
	}
	next := o.snapshotTeams()
	next[slug] = tm
	o.mu.Unlock()

	if err := o.save(next); err != nil {
		return err
	}
	o.commit(func(teams map[string]Team) { teams[slug] = tm },
		Change{Kind: TeamUpdated, Team: slug})
	return nil
}

// DeleteTeam removes a team; membership of agents elsewhere is untouched.
func (o *Org) DeleteTeam(slug string) error {
	o.mu.Lock()
	if _, exists := o.teams[slug]; !exists {
		o.mu.Unlock()
		return fmt.Errorf("org: unknown team %q", slug)
	}
	next := o.snapshotTeams()
	delete(next, slug)
	o.mu.Unlock()

	if err := o.save(next); err != nil {
		return err
	}
	o.commit(func(teams map[string]Team) { delete(teams, slug) },
		Change{Kind: TeamRemoved, Team: slug})
	return nil
}

func normalizeTeam(lead string, members []string) (Team, error) {
	tm := Team{}
	if lead = strings.TrimSpace(lead); lead != "" {
		if !ValidMemberName(lead) {
			return tm, fmt.Errorf("org: bad lead %q", lead)
		}
		tm.Lead = lead
	}
	seen := map[string]bool{}
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !ValidMemberName(m) {
			return tm, fmt.Errorf("org: bad member %q", m)
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		tm.Members = append(tm.Members, m)
	}
	sort.Strings(tm.Members)
	return tm, nil
}

func (o *Org) snapshotTeams() map[string]Team {
	out := make(map[string]Team, len(o.teams))
	for k, v := range o.teams {
		out[k] = v.clone()
	}
	return out
}

// save persists the candidate state atomically without touching memory;
// commit applies it after success so disk and memory never diverge.
func (o *Org) save(candidate map[string]Team) error {
	f := file{Schema: SchemaVersion, Teams: map[string]team{}}
	for slug, t := range candidate {
		f.Teams[slug] = team{Lead: t.Lead, Members: t.Members}
	}
	path := filepath.Join(o.root, File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("org: write %s: %w", File, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".org-*.toml")
	if err != nil {
		return fmt.Errorf("org: write %s: %w", File, err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(f); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("org: encode %s: %w", File, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("org: write %s: %w", File, err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("org: write %s: %w", File, err)
	}
	return nil
}

// commit swaps in the persisted state and fans out the change.
func (o *Org) commit(apply func(map[string]Team), c Change) {
	o.mu.Lock()
	apply(o.teams)
	targets := make([]chan Change, 0, len(o.subs))
	for _, sub := range o.subs {
		targets = append(targets, sub)
	}
	o.mu.Unlock()
	for _, sub := range targets {
		select {
		case sub <- c:
		default:
		}
	}
}

// Subscribe receives subsequent org changes until cancel runs.
func (o *Org) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 8)
	o.mu.Lock()
	o.subSeq++
	id := o.subSeq
	if o.subs == nil {
		o.subs = map[int]chan Change{}
	}
	o.subs[id] = ch
	o.mu.Unlock()
	return ch, func() {
		o.mu.Lock()
		delete(o.subs, id)
		o.mu.Unlock()
	}
}
