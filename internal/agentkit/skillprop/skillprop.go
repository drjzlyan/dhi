// Package skillprop holds the skills employees suggest installing
// (F-091, ADR-0039, ADR-0043). A suggestion names where a skill is
// published — a GitHub or git URL, or a plugin in a marketplace; DHI
// fetches it, pins the commit, and records what the human needs to judge
// it: every skill's instructions and every script in full. Approving
// installs exactly that commit for this workspace or every workspace.
package skillprop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// File is the proposals file under the workspace.
const File = ".dhi/proposals/skills.json"

// Statuses.
const (
	Pending  = "pending"
	Approved = "approved"
	Rejected = "rejected"
)

// Scopes an approval installs to.
const (
	ScopeWorkspace  = "this workspace"
	ScopeEverywhere = "every workspace"
)

// SkillInfo is one skill as the reviewer sees it.
type SkillInfo struct {
	Slug        string       `json:"slug"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Body        string       `json:"body"` // what the agent reads when the skill applies
	Scripts     []ScriptInfo `json:"scripts,omitempty"`
}

// ScriptInfo is one file the skill could run, in full.
type ScriptInfo struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// Proposal is one suggestion and its review.
type Proposal struct {
	ID      int         `json:"id"`
	Entry   string      `json:"entry"`
	Version string      `json:"version"`
	Source  string      `json:"source"`
	Skills  []SkillInfo `json:"skills"`
	Ignored []string    `json:"ignored,omitempty"` // what a skill install leaves out
	Agent   string      `json:"agent"`
	Reason  string      `json:"reason"`
	Status  string      `json:"status"`
	Scope   string      `json:"scope,omitempty"`
	Created time.Time   `json:"created"`
	Decided time.Time   `json:"decided,omitzero"`
}

// HasScripts reports a suggestion that ships executable code.
func (p Proposal) HasScripts() bool {
	for _, k := range p.Skills {
		if len(k.Scripts) > 0 {
			return true
		}
	}
	return false
}

// Trust names how the content is pinned.
func (p Proposal) Trust() string {
	if p.Version != "" {
		return "pinned to commit " + short(p.Version) + "; installs exactly what you review"
	}
	return "a local folder: installs what is there when you approve"
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// Store persists proposals for one workspace.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open binds the store to the workspace at root.
func Open(root string) *Store { return &Store{path: filepath.Join(root, File)} }

func (s *Store) load() ([]Proposal, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Proposal
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("skillprop: %s: %w", s.path, err)
	}
	return out, nil
}

func (s *Store) save(ps []Proposal) error {
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Propose records p as pending; a pending suggestion of the same entry
// is replaced (the newest reason and review win).
func (s *Store) Propose(p Proposal) (Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.load()
	if err != nil {
		return p, err
	}
	next := 1
	kept := ps[:0]
	for _, q := range ps {
		next = max(next, q.ID+1)
		if q.Status == Pending && q.Entry == p.Entry {
			continue
		}
		kept = append(kept, q)
	}
	p.ID, p.Status, p.Created = next, Pending, time.Now().UTC()
	return p, s.save(append(kept, p))
}

// Pending lists open suggestions, oldest first.
func (s *Store) Pending() []Proposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, _ := s.load()
	var out []Proposal
	for _, p := range ps {
		if p.Status == Pending {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Resolve records the human's decision (and where it installed).
func (s *Store) Resolve(id int, status, scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.load()
	if err != nil {
		return err
	}
	for i := range ps {
		if ps[i].ID == id {
			ps[i].Status, ps[i].Scope, ps[i].Decided = status, scope, time.Now().UTC()
			return s.save(ps)
		}
	}
	return fmt.Errorf("skillprop: no suggestion #%d", id)
}

// Review fetches source (a GitHub URL, a git URL, or a plugin from a
// marketplace) and builds the record a human judges: every skill's
// instructions and every script in full, pinned to the commit fetched.
func Review(ctx context.Context, fetch install.Fetcher, tmp string, source install.Source) (Proposal, error) {
	b, err := install.Preview(ctx, source, fetch, tmp)
	if err != nil {
		return Proposal{}, err
	}
	defer b.Close()
	if b.Marketplace != nil {
		return Proposal{}, fmt.Errorf("%s is a marketplace; suggest one of its plugins", source)
	}
	if len(b.Skills) == 0 {
		return Proposal{}, fmt.Errorf("%s ships no skills", source)
	}
	pinned := source
	if b.Commit != "" {
		pinned.Ref = b.Commit
	}
	p := Proposal{Entry: pinned.String(), Version: b.Commit, Source: source.String()}
	for _, c := range b.Skills {
		info := SkillInfo{Slug: c.Skill.Slug, Name: c.Skill.Name, Description: c.Skill.Description, Body: c.Skill.Body}
		for _, rel := range c.Scripts {
			text, _ := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(rel)))
			info.Scripts = append(info.Scripts, ScriptInfo{Path: rel, Text: string(text)})
		}
		p.Skills = append(p.Skills, info)
	}
	if len(b.Servers) > 0 {
		p.Ignored = append(p.Ignored, fmt.Sprintf("it also configures %d MCP server(s); add those from Settings → INTEGRATIONS", len(b.Servers)))
	}
	return p, nil
}

// Install fetches exactly the reviewed commit and installs p's skills
// (scripts allowed: the human read them) into scope. It returns the
// skill names.
func Install(ctx context.Context, fetch install.Fetcher, tmp string, p Proposal, scope install.Scope) ([]string, error) {
	src, err := install.ParseSource(p.Entry)
	if err != nil {
		return nil, err
	}
	b, err := install.Preview(ctx, src, fetch, tmp)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	if p.Version != "" && b.Commit != p.Version {
		return nil, fmt.Errorf("%s changed since it was reviewed (commit %s, reviewed %s); ask for a new suggestion", p.Source, b.Commit, p.Version)
	}
	var names []string
	for _, k := range p.Skills {
		names = append(names, k.Slug)
	}
	b.Servers = nil // a suggestion installs skills only
	res, err := install.Apply(b, scope, &install.Pick{Skills: names}, true)
	return res.Skills, err
}

// Attach adds slugs to an employee's skills (they ride in its prompt).
func Attach(ws *workspace.Workspace, o *org.Org, agentID string, slugs []string) error {
	roster, err := org.LoadRoster(ws)
	if err != nil {
		return err
	}
	for _, a := range roster {
		if a.ID != agentID {
			continue
		}
		cp := *a
		cp.Skills = append([]string(nil), a.Skills...)
		for _, s := range slugs {
			if !contains(cp.Skills, s) {
				cp.Skills = append(cp.Skills, s)
			}
		}
		return o.UpdateAgent(ws, &cp)
	}
	return fmt.Errorf("unknown employee %q", agentID)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
