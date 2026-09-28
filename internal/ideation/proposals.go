package ideation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ProposalDecision is the lifecycle of an agent's session proposal.
type ProposalDecision string

// Decisions. A pending proposal awaits the human; accepting it creates the
// session (and records CreatedSlug), declining creates nothing. Neither
// decision ever resurrects (F-033 Part A).
const (
	ProposalPending  ProposalDecision = "pending"
	ProposalAccepted ProposalDecision = "accepted"
	ProposalDeclined ProposalDecision = "declined"
)

// Valid reports whether d is a defined decision.
func (d ProposalDecision) Valid() bool {
	switch d {
	case ProposalPending, ProposalAccepted, ProposalDeclined:
		return true
	}
	return false
}

// Proposal is an agent's request to open a session or breakout. Agents
// never open one directly — only the human accepts a proposal, which is
// what makes the front door a human decision (F-033 acceptance 3).
type Proposal struct {
	ID           int              `toml:"id"`
	Caller       string           `toml:"caller"`
	Name         string           `toml:"name"`
	Topic        string           `toml:"topic"`
	Mode         SessionMode      `toml:"mode"`
	Parent       string           `toml:"parent,omitempty"`
	Participants []string         `toml:"participants"`
	Decision     ProposalDecision `toml:"decision"`
	CreatedSlug  string           `toml:"created_slug,omitempty"`
	At           time.Time        `toml:"at"`
}

// propFile is the on-disk TOML shape of the proposal registry.
type propFile struct {
	Proposals []Proposal `toml:"proposal"`
}

// proposalsPath is the registry file.
func (s *Store) proposalsPath() string {
	return filepath.Join(s.ws.Root, Dir, proposalsFile)
}

// loadProposals reads the registry, if present. Malformed rows are
// recorded as warnings, never fatal (ADR-0011 strict data: visible).
func (s *Store) loadProposals() {
	s.propSeq = 0
	path := s.proposalsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		s.warns = append(s.warns, fmt.Sprintf("proposals: %v", err))
		return
	}
	var f propFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		s.warns = append(s.warns, fmt.Sprintf("proposals: parse: %v", err))
		return
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		s.warns = append(s.warns, fmt.Sprintf("proposals: unknown keys: %v", undecoded))
		return
	}
	for _, p := range f.Proposals {
		if p.ID <= 0 || strings.TrimSpace(p.Caller) == "" || !p.Mode.Valid() || !p.Decision.Valid() {
			s.warns = append(s.warns, fmt.Sprintf("proposals: malformed row id=%d", p.ID))
			continue
		}
		s.proposals = append(s.proposals, p)
		if p.ID >= s.propSeq {
			s.propSeq = p.ID + 1
		}
	}
	sort.Slice(s.proposals, func(i, j int) bool { return s.proposals[i].ID < s.proposals[j].ID })
}

// Propose records a pending session/breakout proposal from an agent. The
// request is durable until the human accepts or declines it.
func (s *Store) Propose(caller, name, topic string, mode SessionMode, parent string, participants []string) (Proposal, error) {
	caller = strings.TrimSpace(caller)
	name = strings.TrimSpace(name)
	if caller == "" {
		return Proposal{}, fmt.Errorf("ideation: proposal needs a caller")
	}
	if name == "" {
		return Proposal{}, fmt.Errorf("ideation: proposal needs a name")
	}
	if mode == "" {
		mode = ModeGroup
	}
	if !mode.Valid() {
		return Proposal{}, fmt.Errorf("ideation: bad mode %q (want 1:1, group, or breakout)", mode)
	}
	parent = strings.TrimSpace(parent)
	if mode == ModeBreakout {
		if parent == "" {
			return Proposal{}, fmt.Errorf("ideation: breakout proposal needs a parent")
		}
		if _, ok := s.Get(parent); !ok {
			return Proposal{}, fmt.Errorf("ideation: parent session %q not found", parent)
		}
	} else if parent != "" {
		return Proposal{}, fmt.Errorf("ideation: parent set on a %s proposal", mode)
	}
	invited := dedupeSorted(participants)
	if mode == ModeOneOnOne && len(invited) != 1 {
		return Proposal{}, fmt.Errorf("ideation: 1:1 proposal needs exactly one participant, got %d", len(invited))
	}
	s.mu.Lock()
	s.propSeq++
	p := Proposal{
		ID: s.propSeq, Caller: caller, Name: name, Topic: strings.TrimSpace(topic),
		Mode: mode, Parent: parent, Participants: invited,
		Decision: ProposalPending, At: s.now(),
	}
	s.mu.Unlock()
	if err := s.writeProposals(append(s.snapshotProposals(), p)); err != nil {
		return Proposal{}, err
	}
	s.commitProposals(func() { s.proposals = append(s.proposals, p) }, Change{Kind: ProposalAdded, ID: itoaInt(p.ID)})
	return p, nil
}

// Proposals returns the full registry in id order.
func (s *Store) Proposals() []Proposal {
	return s.snapshotProposals()
}

// PendingProposals returns the proposals still awaiting a human decision.
func (s *Store) PendingProposals() []Proposal {
	var out []Proposal
	for _, p := range s.snapshotProposals() {
		if p.Decision == ProposalPending {
			out = append(out, p)
		}
	}
	return out
}

// Decision resolves one pending proposal. createdSlug is recorded for an
// accepted proposal (the session/breakout the human opened). A decided
// proposal cannot be re-decided.
func (s *Store) Decision(id int, decision ProposalDecision, createdSlug string) error {
	if decision != ProposalAccepted && decision != ProposalDeclined {
		return fmt.Errorf("ideation: decision must be accepted or declined, got %q", decision)
	}
	s.mu.RLock()
	idx := -1
	for i := range s.proposals {
		if s.proposals[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.RUnlock()
		return fmt.Errorf("ideation: unknown proposal %d", id)
	}
	if s.proposals[idx].Decision != ProposalPending {
		cur := s.proposals[idx].Decision
		s.mu.RUnlock()
		return fmt.Errorf("ideation: proposal %d is already %s", id, cur)
	}
	next := append([]Proposal(nil), s.proposals...)
	next[idx].Decision = decision
	next[idx].CreatedSlug = strings.TrimSpace(createdSlug)
	s.mu.RUnlock()

	if err := s.writeProposals(next); err != nil {
		return err
	}
	s.commitProposals(func() { s.proposals = next }, Change{Kind: ProposalDecided, ID: itoaInt(id)})
	return nil
}

func (s *Store) snapshotProposals() []Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Proposal(nil), s.proposals...)
}

// writeProposals persists the registry atomically (temp + rename).
func (s *Store) writeProposals(list []Proposal) error {
	path := s.proposalsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ideation: write proposals: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".proposals-*.toml")
	if err != nil {
		return fmt.Errorf("ideation: write proposals: %w", err)
	}
	name := tmp.Name()
	if err := toml.NewEncoder(tmp).Encode(propFile{Proposals: list}); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("ideation: encode proposals: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("ideation: write proposals: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("ideation: write proposals: %w", err)
	}
	return nil
}

// commitProposals applies the persisted state under lock and fans out.
func (s *Store) commitProposals(apply func(), c Change) {
	s.mu.Lock()
	apply()
	targets := make([]chan Change, 0, len(s.subs))
	for _, sub := range s.subs {
		targets = append(targets, sub)
	}
	s.mu.Unlock()
	for _, sub := range targets {
		select {
		case sub <- c:
		default:
		}
	}
}

func itoaInt(n int) string { return fmt.Sprintf("%d", n) }
