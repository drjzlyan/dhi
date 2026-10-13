// Package setprop holds agents' proposed settings changes (F-070): an
// agent that finds a setting in its way (a budget too tight, a scope that
// keeps asking, a missing worktree root) proposes the change with a
// reason; only the human applies it, from Settings → PROPOSALS.
package setprop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Layers a proposal may target.
const (
	LayerWorkspace = "workspace" // .dhi/config.toml (the default)
	LayerUser      = "user"      // ~/.config/dhi/config.toml — only if the human picks it
	// LayerProject targets the project brief (F-080): Key is
	// "project.<field>", applied to .dhi/project.toml.
	LayerProject = "project"
)

// Statuses.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// Proposal is one proposed change.
type Proposal struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Old       string    `json:"old"`
	Layer     string    `json:"layer"`
	Agent     string    `json:"agent"`
	Rationale string    `json:"rationale"`
	Status    string    `json:"status"`
	Created   time.Time `json:"created"`
	Resolved  time.Time `json:"resolved,omitzero"`
}

// Store keeps proposals in .dhi/proposals/settings.json.
type Store struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
}

// Open roots the store at wsRoot.
func Open(wsRoot string) *Store {
	return &Store{path: filepath.Join(wsRoot, ".dhi", "proposals", "settings.json"), now: time.Now}
}

func (s *Store) load() ([]Proposal, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("setprop: %w", err)
	}
	var ps []Proposal
	if err := json.Unmarshal(b, &ps); err != nil {
		return nil, fmt.Errorf("setprop: %s: %w", s.path, err)
	}
	return ps, nil
}

func (s *Store) save(ps []Proposal) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("setprop: %w", err)
	}
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("setprop: %w", err)
	}
	return os.Rename(tmp, s.path)
}

// Propose records a pending proposal. A pending proposal for the same
// key and layer is replaced (the latest reasoning wins), never stacked.
func (s *Store) Propose(p Proposal) (Proposal, error) {
	if p.Layer == "" {
		p.Layer = LayerWorkspace
	}
	if p.Layer != LayerWorkspace && p.Layer != LayerUser && p.Layer != LayerProject {
		return Proposal{}, fmt.Errorf("setprop: layer %q (want %s, %s or %s)", p.Layer, LayerWorkspace, LayerUser, LayerProject)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.load()
	if err != nil {
		return Proposal{}, err
	}
	p.Created, p.Status = s.now().UTC(), StatusPending
	h := sha256.Sum256([]byte(p.Key + "|" + p.Layer + "|" + p.Value + "|" + p.Created.String()))
	p.ID = "s-" + hex.EncodeToString(h[:4])
	kept := ps[:0]
	for _, q := range ps {
		if q.Status != StatusPending || q.Key != p.Key || q.Layer != p.Layer {
			kept = append(kept, q)
		}
	}
	return p, s.save(append(kept, p))
}

// Pending lists proposals awaiting the human, oldest first.
func (s *Store) Pending() ([]Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.load()
	if err != nil {
		return nil, err
	}
	var out []Proposal
	for _, p := range ps {
		if p.Status == StatusPending {
			out = append(out, p)
		}
	}
	return out, nil
}

// Resolve marks a proposal approved or rejected (the record is kept).
func (s *Store) Resolve(id, status string) error {
	if status != StatusApproved && status != StatusRejected {
		return fmt.Errorf("setprop: status %q", status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.load()
	if err != nil {
		return err
	}
	for i := range ps {
		if ps[i].ID == id && ps[i].Status == StatusPending {
			ps[i].Status, ps[i].Resolved = status, s.now().UTC()
			return s.save(ps)
		}
	}
	return fmt.Errorf("setprop: no pending proposal %s", id)
}
