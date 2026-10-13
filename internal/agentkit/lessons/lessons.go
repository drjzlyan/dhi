// Package lessons is an agent's learned, reusable knowledge (F-068,
// ADR-0033): short imperative rules distilled from real work ("this
// repo's tests need -race", "the human prefers small PRs"), each with
// provenance, a confidence, and use counts. Private lessons belong to one
// agent; user lessons (the human's preferences) apply to every agent.
// Lessons are retrieved by relevance into the prompt, consolidated
// locally (dedupe, decay — no tokens spent), and stay fully reviewable:
// the human can pin, edit or retire any of them.
package lessons

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/drjzlyan/dhi/internal/fsx"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/knowledge"
)

// Scopes.
const (
	ScopePrivate = "private" // one agent's own
	ScopeUser    = "user"    // the human's preferences: every agent
)

// Statuses.
const (
	StatusActive  = "active"
	StatusPinned  = "pinned"  // always injected, never decays
	StatusRetired = "retired" // kept for the record, never injected
)

// Sources.
const (
	SourceReflection = "reflection" // DHI's post-run reflection
	SourceCorrection = "correction" // distilled from a human correction
	SourceAgent      = "agent"      // the agent recorded it (lesson_record)
	SourceHuman      = "human"      // written or edited in the Brain
)

// Lesson is one learned rule.
type Lesson struct {
	ID         string    `json:"id"`
	Agent      string    `json:"agent,omitempty"` // owner; "" for user scope
	Scope      string    `json:"scope"`
	Text       string    `json:"text"`
	Tags       []string  `json:"tags,omitempty"`
	Confidence float64   `json:"confidence"` // 0..1
	Source     string    `json:"source"`
	Runs       []string  `json:"runs,omitempty"` // provenance: run ids
	Uses       int       `json:"uses"`
	LastUsed   time.Time `json:"last_used,omitzero"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
	Status     string    `json:"status"`
}

func (l Lesson) live() bool { return l.Status == StatusActive || l.Status == StatusPinned }

// Store keeps lessons under .dhi/memory: agents/<id>/lessons.json and
// user-lessons.json.
type Store struct {
	root string // .dhi/memory
	mu   sync.Mutex
	now  func() time.Time
}

// Open roots the store at wsRoot.
func Open(wsRoot string) *Store {
	return &Store{root: filepath.Join(wsRoot, ".dhi", "memory"), now: time.Now}
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func (s *Store) path(scope, agent string) (string, error) {
	switch scope {
	case ScopeUser:
		return filepath.Join(s.root, "user-lessons.json"), nil
	case ScopePrivate:
		if !idRe.MatchString(agent) {
			return "", fmt.Errorf("lessons: bad agent id %q", agent)
		}
		return filepath.Join(s.root, "agents", agent, "lessons.json"), nil
	}
	return "", fmt.Errorf("lessons: unknown scope %q (want %s or %s)", scope, ScopePrivate, ScopeUser)
}

func (s *Store) load(p string) ([]Lesson, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lessons: %w", err)
	}
	var ls []Lesson
	if err := json.Unmarshal(b, &ls); err != nil {
		return nil, fmt.Errorf("lessons: %s: %w", p, err)
	}
	return ls, nil
}

func (s *Store) save(p string, ls []Lesson) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("lessons: %w", err)
	}
	b, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(p, b, 0o644)
}

// mutate applies fn to one file's lessons under the lock.
func (s *Store) mutate(scope, agent string, fn func([]Lesson) ([]Lesson, error)) error {
	p, err := s.path(scope, agent)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file lock covers another DHI process on this workspace (F-078):
	// its reflection must not overwrite this one's new lesson.
	return fsx.Locked(p, func() error {
		ls, err := s.load(p)
		if err != nil {
			return err
		}
		out, err := fn(ls)
		if err != nil {
			return err
		}
		return s.save(p, out)
	})
}

// norm canonicalises lesson text for identity and dedupe.
func norm(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimRight(strings.TrimSpace(text), "."))), " ")
}

func idFor(scope, agent, text string) string {
	h := sha256.Sum256([]byte(scope + "|" + agent + "|" + norm(text)))
	return "l-" + hex.EncodeToString(h[:4])
}

// dupThreshold is the similarity above which two lessons are one.
const dupThreshold = 0.75

// Add records a lesson, merging it into a near-duplicate when one exists
// (confidence rises, provenance accumulates). It returns the stored
// lesson and whether it was new.
func (s *Store) Add(l Lesson) (Lesson, bool, error) {
	l.Text = strings.TrimSpace(l.Text)
	if l.Text == "" || len([]rune(l.Text)) > 400 {
		return Lesson{}, false, fmt.Errorf("lessons: text must be 1..400 characters")
	}
	if l.Scope == "" {
		l.Scope = ScopePrivate
	}
	if l.Scope == ScopeUser {
		l.Agent = ""
	}
	l.Confidence = clamp(l.Confidence, 0.1, 1)
	now := s.now().UTC()
	var stored Lesson
	added := false
	err := s.mutate(l.Scope, l.Agent, func(ls []Lesson) ([]Lesson, error) {
		if i := nearest(ls, l.Text); i >= 0 {
			ex := &ls[i]
			ex.Confidence = clamp(ex.Confidence+0.15*(1-ex.Confidence)+0.05, 0, 1)
			ex.Runs = appendUnique(ex.Runs, l.Runs...)
			ex.Tags = appendUnique(ex.Tags, l.Tags...)
			ex.Updated = now
			if ex.Status == StatusRetired {
				ex.Status = StatusActive // learned again: it was not wrong after all
			}
			stored = *ex
			return ls, nil
		}
		l.ID = idFor(l.Scope, l.Agent, l.Text)
		l.Created, l.Updated = now, now
		if l.Status == "" {
			l.Status = StatusActive
		}
		stored, added = l, true
		return append(ls, l), nil
	})
	return stored, added, err
}

// nearest is the index of the live-or-retired lesson most similar to
// text above dupThreshold, or -1.
func nearest(ls []Lesson, text string) int {
	if len(ls) == 0 {
		return -1
	}
	docs := make([]string, len(ls))
	for i, l := range ls {
		if norm(l.Text) == norm(text) {
			return i
		}
		docs[i] = l.Text
	}
	best, at := 0.0, -1
	for i, sc := range knowledge.Similarity(docs, text) {
		if sc > best {
			best, at = sc, i
		}
	}
	if best >= dupThreshold {
		return at
	}
	return -1
}

// List returns one agent's private lessons plus the user lessons
// (agent "" = user lessons only), newest first.
func (s *Store) List(agent string) ([]Lesson, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Lesson
	if agent != "" {
		p, err := s.path(ScopePrivate, agent)
		if err != nil {
			return nil, err
		}
		ls, err := s.load(p)
		if err != nil {
			return nil, err
		}
		out = append(out, ls...)
	}
	p, _ := s.path(ScopeUser, "")
	us, err := s.load(p)
	if err != nil {
		return nil, err
	}
	out = append(out, us...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Retrieve picks the lessons for a turn: every pinned lesson, then the
// best k live ones by relevance to query (similarity, confidence,
// recency). It does not count a use: prompts are rebuilt within a turn
// (compaction, fallback, verify rounds) — the caller marks once with
// MarkUsed.
func (s *Store) Retrieve(agent, query string, k int) ([]Lesson, error) {
	all, err := s.List(agent)
	if err != nil {
		return nil, err
	}
	var pinned, live []Lesson
	for _, l := range all {
		switch {
		case l.Status == StatusPinned:
			pinned = append(pinned, l)
		case l.live():
			live = append(live, l)
		}
	}
	now := s.now()
	if len(live) > 0 {
		docs := make([]string, len(live))
		for i, l := range live {
			docs[i] = l.Text + " " + strings.Join(l.Tags, " ")
		}
		sims := knowledge.Similarity(docs, query)
		score := func(i int) float64 {
			age := now.Sub(live[i].Updated).Hours() / 24
			recency := 1 / (1 + age/30)
			return 0.6*sims[i] + 0.25*live[i].Confidence + 0.15*recency
		}
		idx := make([]int, len(live))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return score(idx[a]) > score(idx[b]) })
		var picked []Lesson
		for _, i := range idx {
			// A lesson that shares nothing with the task is noise unless
			// the agent is confident it always applies.
			if len(picked) >= k || sims[i] < 0.05 && live[i].Confidence < 0.8 {
				continue
			}
			picked = append(picked, live[i])
		}
		live = picked
	}
	return append(pinned, live...), nil
}

// MarkUsed counts one use of each lesson (once per turn).
func (s *Store) MarkUsed(ls []Lesson) {
	now := s.now().UTC()
	byFile := map[[2]string]map[string]bool{}
	for _, l := range ls {
		key := [2]string{l.Scope, l.Agent}
		if byFile[key] == nil {
			byFile[key] = map[string]bool{}
		}
		byFile[key][l.ID] = true
	}
	for key, ids := range byFile {
		_ = s.mutate(key[0], key[1], func(all []Lesson) ([]Lesson, error) {
			for i := range all {
				if ids[all[i].ID] {
					all[i].Uses++
					all[i].LastUsed = now
				}
			}
			return all, nil
		})
	}
}

// Set changes a lesson's status (pin, retire, reactivate) or text (an
// edit in the Brain, which makes it the human's own).
func (s *Store) Set(scope, agent, id string, fn func(*Lesson)) error {
	return s.mutate(scope, agent, func(ls []Lesson) ([]Lesson, error) {
		for i := range ls {
			if ls[i].ID == id {
				fn(&ls[i])
				ls[i].Updated = s.now().UTC()
				return ls, nil
			}
		}
		return nil, fmt.Errorf("lessons: no lesson %s", id)
	})
}

// Report says what a consolidation pass changed.
type Report struct{ Merged, Retired int }

// Consolidate merges near-duplicates and retires lessons that have gone
// stale: unused for staleDays with low confidence. Pinned lessons never
// decay. Local and token-free.
func (s *Store) Consolidate(agent string, staleDays int) (Report, error) {
	var rep Report
	now := s.now()
	pass := func(ls []Lesson) ([]Lesson, error) {
		var out []Lesson
		for _, l := range ls {
			if l.live() {
				if i := nearest(out, l.Text); i >= 0 && out[i].live() {
					out[i].Confidence = clamp(max(out[i].Confidence, l.Confidence)+0.05, 0, 1)
					out[i].Runs = appendUnique(out[i].Runs, l.Runs...)
					out[i].Uses += l.Uses
					rep.Merged++
					continue
				}
			}
			last := l.LastUsed
			if last.IsZero() {
				last = l.Created
			}
			if l.Status == StatusActive && l.Confidence < 0.5 && now.Sub(last) > time.Duration(staleDays)*24*time.Hour {
				l.Status = StatusRetired
				rep.Retired++
			}
			out = append(out, l)
		}
		return out, nil
	}
	if agent != "" {
		if err := s.mutate(ScopePrivate, agent, pass); err != nil {
			return rep, err
		}
	}
	return rep, s.mutate(ScopeUser, "", pass)
}

func clamp(v, lo, hi float64) float64 { return min(max(v, lo), hi) }

func appendUnique(xs []string, add ...string) []string {
	seen := map[string]bool{}
	for _, x := range xs {
		seen[x] = true
	}
	for _, a := range add {
		if a != "" && !seen[a] {
			xs = append(xs, a)
			seen[a] = true
		}
	}
	return xs
}
