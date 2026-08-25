// Package knowledge implements the shared workspace KB (ADR-0007):
// markdown entries plus an index.json carrying provenance for every
// record. Retrieval is managed-ripgrep over the corpus scored by
// recency/importance behind the KnowledgeStore interface, so an
// embedding-backed store can replace it later. Contributions follow a
// per-workspace policy: auto publishes immediately, review (the default)
// parks them in a pending queue until a human approves.
package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Status is what happened to a contribution.
type Status string

// Contribution outcomes.
const (
	StatusPublished Status = "published"
	StatusQueued    Status = "queued"
)

// Policy governs contributions.
type Policy string

// Policies.
const (
	Auto   Policy = "auto"
	Review Policy = "review"
)

// Entry is one KB record with full provenance.
type Entry struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	File       string    `json:"file"` // relative to the KB root
	Tags       []string  `json:"tags,omitempty"`
	Author     string    `json:"author"`
	Created    time.Time `json:"created"`
	Importance int       `json:"importance"` // 1..5
}

// Contribution is a proposed new entry.
type Contribution struct {
	Title      string
	Body       string
	Tags       []string
	Author     string
	Importance int // 1..5; zero means 3
}

// Hit is one scored retrieval result.
type Hit struct {
	Entry   Entry
	Score   float64
	Snippet string
}

// KnowledgeStore is the retrieval/contribution seam (embedding-backed
// implementations may replace Store).
type KnowledgeStore interface {
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
	Contribute(c Contribution) (Status, string, error)
	Pending() ([]Queued, error)
	Approve(id string) (Entry, error)
}

// Queued is a contribution awaiting review.
type Queued struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Tags       []string  `json:"tags,omitempty"`
	Author     string    `json:"author"`
	Importance int       `json:"importance,omitempty"`
	At         time.Time `json:"at"`
}

type index struct {
	Entries []Entry `json:"entries"`
}

// Store is the rg-backed KnowledgeStore implementation.
type Store struct {
	root     string // .dhi/knowledge
	policy   Policy
	searcher search.Searcher

	mu    sync.Mutex
	index index
}

// Open loads (or initializes) the KB under ws with the given policy;
// zero policy means Review.
func Open(ws *workspace.Workspace, pol Policy, searcher search.Searcher) (*Store, error) {
	if pol != Auto && pol != Review {
		return nil, fmt.Errorf("knowledge: unknown policy %q", pol)
	}
	if pol == "" {
		pol = Review
	}
	s := &Store{root: filepath.Join(ws.Root, workspace.DirKnowledge), policy: pol, searcher: searcher}
	for _, d := range []string{"entries", "pending"} {
		if err := os.MkdirAll(filepath.Join(s.root, d), 0o755); err != nil {
			return nil, fmt.Errorf("knowledge: mkdir %s: %w", d, err)
		}
	}
	data, err := os.ReadFile(s.indexPath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge: read index: %w", err)
	}
	if err := json.Unmarshal(data, &s.index); err != nil {
		return nil, fmt.Errorf("knowledge: parse index: %w", err)
	}
	sortEntries(s.index.Entries)
	return s, nil
}

func (s *Store) indexPath() string  { return filepath.Join(s.root, "index.json") }
func (s *Store) entriesDir() string { return filepath.Join(s.root, "entries") }
func (s *Store) pendingDir() string { return filepath.Join(s.root, "pending") }

// Search retrieves up to limit hits scored by term matches, recency,
// and importance. A nil searcher yields no results (KB still works for
// writes).
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" || s.searcher == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, err := s.searcher.Search(ctx, query, []string{s.entriesDir()})
	if err != nil {
		return nil, fmt.Errorf("knowledge: search: %w", err)
	}
	type agg struct {
		hits    int
		snippet string
	}
	byFile := map[string]*agg{}
	for h := range ch {
		a := byFile[h.Path]
		if a == nil {
			a = &agg{snippet: h.Text}
			byFile[h.Path] = a
		}
		a.hits++
	}

	now := time.Now()
	var out []Hit
	for _, e := range s.index.Entries {
		path := filepath.Join(s.root, filepath.FromSlash(e.File))
		a := byFile[path]
		if a == nil {
			continue
		}
		out = append(out, Hit{
			Entry:   e,
			Score:   score(e, a.hits, now),
			Snippet: strings.TrimSpace(a.snippet),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Entry.ID < out[j].Entry.ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// score blends importance, freshness (full credit < 7d, decayed to zero
// at 30d), and match density.
func score(e Entry, hits int, now time.Time) float64 {
	fresh := 0.0
	days := now.Sub(e.Created).Hours() / 24
	switch {
	case days < 7:
		fresh = 2.0
	case days < 30:
		fresh = 2.0 * (1 - (days-7)/23)
	}
	importance := float64(e.Importance)
	if importance <= 0 {
		importance = 3
	}
	matchDensity := 0.5 * math.Log1p(float64(hits))
	return importance + fresh + matchDensity
}

// Contribute files c according to policy and returns its reference id.
func (s *Store) Contribute(c Contribution) (Status, string, error) {
	if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Body) == "" {
		return "", "", fmt.Errorf("knowledge: title and body are required")
	}
	if c.Author == "" {
		return "", "", fmt.Errorf("knowledge: author is required")
	}
	id := deriveID(c.Title, c.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policy == Auto {
		e, err := s.publish(id, c, time.Now())
		if err != nil {
			return "", "", err
		}
		_ = e
		return StatusPublished, id, nil
	}
	q := Queued{ID: id, Title: c.Title, Body: c.Body, Tags: c.Tags, Author: c.Author, Importance: c.Importance, At: time.Now()}
	b, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("knowledge: encode pending: %w", err)
	}
	path := filepath.Join(s.pendingDir(), id+".json")
	if err := atomicWrite(path, b); err != nil {
		return "", "", err
	}
	return StatusQueued, id, nil
}

// Pending lists queued contributions oldest-first.
func (s *Store) Pending() ([]Queued, error) {
	files, err := os.ReadDir(s.pendingDir())
	if err != nil {
		return nil, fmt.Errorf("knowledge: list pending: %w", err)
	}
	var out []Queued
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		var q Queued
		b, err := os.ReadFile(filepath.Join(s.pendingDir(), f.Name()))
		if err != nil {
			return nil, fmt.Errorf("knowledge: read %s: %w", f.Name(), err)
		}
		if err := json.Unmarshal(b, &q); err != nil {
			return nil, fmt.Errorf("knowledge: parse %s: %w", f.Name(), err)
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// Approve publishes a pending contribution; unknown ids error.
func (s *Store) Approve(id string) (Entry, error) {
	if !idRe.MatchString(id) {
		return Entry{}, fmt.Errorf("knowledge: bad id %q", id)
	}
	path := filepath.Join(s.pendingDir(), id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, fmt.Errorf("knowledge: pending %s: %w", id, err)
	}
	var q Queued
	if err := json.Unmarshal(b, &q); err != nil {
		return Entry{}, fmt.Errorf("knowledge: parse pending: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.publish(q.ID, Contribution{Title: q.Title, Body: q.Body, Tags: q.Tags, Author: q.Author, Importance: q.Importance}, q.At)
	if err != nil {
		return Entry{}, err
	}
	if err := os.Remove(path); err != nil {
		return e, fmt.Errorf("knowledge: clear pending: %w", err)
	}
	return e, nil
}

// publish writes the body file and updates the in-memory+on-disk index.
// Callers must hold s.mu.
func (s *Store) publish(id string, c Contribution, created time.Time) (Entry, error) {
	file := "entries/" + id + ".md"
	body := "# " + strings.TrimSpace(c.Title) + "\n\n" + strings.TrimRight(c.Body, "\n") + "\n"
	if err := atomicWrite(filepath.Join(s.root, filepath.FromSlash(file)), []byte(body)); err != nil {
		return Entry{}, err
	}
	importance := c.Importance
	if importance <= 0 || importance > 5 {
		importance = 3
	}
	e := Entry{ID: id, Title: strings.TrimSpace(c.Title), File: file, Tags: c.Tags,
		Author: c.Author, Created: created, Importance: importance}
	replaced := false
	for i := range s.index.Entries {
		if s.index.Entries[i].ID == id {
			s.index.Entries[i] = e
			replaced = true
		}
	}
	if !replaced {
		s.index.Entries = append(s.index.Entries, e)
	}
	sortEntries(s.index.Entries)
	b, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return e, fmt.Errorf("knowledge: encode index: %w", err)
	}
	if err := atomicWrite(s.indexPath(), b); err != nil {
		return e, err
	}
	return e, nil
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// deriveID slugs the title and suffixes a short content hash so identical
// titles with different bodies never collide, while identical content
// maps to the same id (idempotent re-contributions).
func deriveID(title, body string) string {
	base := slugUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "entry"
	}
	sum := sha256.Sum256([]byte(title + "\x00" + body))
	return base + "-" + hex.EncodeToString(sum[:3]) // 6 hex chars
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool { return es[i].ID < es[j].ID })
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("knowledge: write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("knowledge: commit %s: %w", filepath.Base(path), err)
	}
	return nil
}

// ContributionsBy returns the entries authored by authorID, newest
// first. It reads the in-memory index when loaded, falling back to disk
// — safe for inspection UIs that run long after Open.
func (s *Store) ContributionsBy(authorID string) []Entry {
	s.mu.Lock()
	entries := append([]Entry(nil), s.index.Entries...)
	s.mu.Unlock()
	if len(entries) == 0 {
		data, err := os.ReadFile(s.indexPath())
		if err == nil {
			var idx index
			if json.Unmarshal(data, &idx) == nil {
				entries = idx.Entries
			}
		}
	}
	var out []Entry
	for _, e := range entries {
		if e.Author == authorID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}
