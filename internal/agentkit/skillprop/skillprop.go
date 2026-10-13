// Package skillprop holds the skills employees suggest installing
// (F-091, ADR-0039). A suggestion names a DHI registry entry; DHI fetches
// it, checks its digest (and signature, when a key is pinned) and records
// what the human needs to judge it: every skill's text and any script in
// full. Approving installs exactly that content — re-fetched and required
// to match the recorded digest — for this workspace or every workspace.
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
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/agentkit/registry"
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
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`             // goes into every prompt it is attached to
	Script      string `json:"script,omitempty"` // path, when it ships one
	ScriptText  string `json:"script_text,omitempty"`
}

// Proposal is one suggestion and its review.
type Proposal struct {
	ID      int         `json:"id"`
	Entry   string      `json:"entry"`
	Version string      `json:"version"`
	Source  string      `json:"source"`
	SHA256  string      `json:"sha256"`
	Signed  bool        `json:"signed"`
	Skills  []SkillInfo `json:"skills"`
	Ignored []string    `json:"ignored,omitempty"` // pack items a skill install leaves out
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
		if k.Script != "" {
			return true
		}
	}
	return false
}

// Trust names how the content was verified.
func (p Proposal) Trust() string {
	if p.Signed {
		return "signed by a publisher you trust, digest verified"
	}
	return "digest verified (no publisher key pinned: unsigned)"
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

// Review fetches entry from reg (digest-verified) and builds the record
// a human judges. A pack with no skills is refused: suggesting agents or
// workflows is not what this is for.
func Review(ctx context.Context, reg *registry.Registry, entry string) (Proposal, error) {
	f, err := reg.Fetch(ctx, entry)
	if err != nil {
		return Proposal{}, err
	}
	defer f.Close()
	payloads, spec, err := pack.ReadSkills(f.Dir)
	if err != nil {
		return Proposal{}, err
	}
	if len(payloads) == 0 {
		return Proposal{}, fmt.Errorf("%s ships no skills (it is a pack of %s); install packs from Settings → MARKETPLACE", entry, packKinds(spec))
	}
	p := Proposal{Entry: entry, Version: f.Entry.Version, Source: f.Entry.Source, SHA256: f.Entry.SHA256, Signed: f.Signed}
	for _, k := range payloads {
		p.Skills = append(p.Skills, SkillInfo{Slug: k.Slug, Name: k.Skill.Name, Description: k.Skill.Description,
			Body: k.Skill.Body, Script: k.Skill.Script, ScriptText: string(k.Script)})
	}
	if kinds := packKinds(spec); kinds != "skills" {
		p.Ignored = append(p.Ignored, "the pack also has "+kinds+"; a skill install adds only its skills")
	}
	return p, nil
}

func packKinds(s *pack.Spec) string {
	var k []string
	add := func(n int, what string) {
		if n > 0 {
			k = append(k, fmt.Sprintf("%d %s", n, what))
		}
	}
	add(len(s.Agents), "agents")
	add(len(s.Workflows), "workflows")
	add(len(s.Roles), "roles")
	add(len(s.Standards), "standards")
	add(len(s.MCPServers), "MCP servers")
	if len(k) == 0 {
		return "skills"
	}
	return strings.Join(k, ", ")
}

// Install re-fetches p's entry, requires the digest the human reviewed,
// and writes its skills (with scripts) to this workspace's .dhi/skills or
// to library.UserSkillsDir for every workspace. It returns the slugs.
func Install(ctx context.Context, reg *registry.Registry, ws *workspace.Workspace, p Proposal, scope string) ([]string, error) {
	f, err := reg.Fetch(ctx, p.Entry)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !strings.EqualFold(f.Entry.SHA256, p.SHA256) {
		return nil, fmt.Errorf("%s changed since it was reviewed (digest %s, reviewed %s); ask for a new suggestion", p.Entry, f.Entry.SHA256, p.SHA256)
	}
	payloads, _, err := pack.ReadSkills(f.Dir)
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(ws.Root, workspace.DirSkills)
	if scope == ScopeEverywhere {
		if library.UserSkillsDir == "" {
			return nil, fmt.Errorf("no user config dir: install for this workspace instead")
		}
		dest = library.UserSkillsDir
	}
	if err := pack.WriteSkills(dest, payloads); err != nil {
		return nil, err
	}
	var slugs []string
	for _, k := range payloads {
		slugs = append(slugs, k.Slug)
	}
	return slugs, nil
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
