// Package channelmeta stores the human-facing Slack-depth layer over the
// append-only message bus (F-035 Part C): reactions, message edits and
// pins. The bus JSONL stays immutable (a message is a fact); this store
// is the mutable view keyed by channel + message id, persisted as one
// atomic JSON document under .dhi/channels/meta.json. Agent chatter is
// never edited — only the human's own messages are.
package channelmeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/drjzlyan/dhi/internal/workspace"
)

// File is the metadata document under the workspace root.
const File = ".dhi/channels/meta.json"

// doc is the on-disk shape.
type doc struct {
	Schema    int                       `json:"schema"`
	Reactions map[string]map[string]int `json:"reactions,omitempty"` // "ch|id" → token → count
	Edits     map[string]string         `json:"edits,omitempty"`     // "ch|id" → new text
	Pins      map[string]bool           `json:"pins,omitempty"`      // "ch|id" → pinned
}

// Store is the mutable channel metadata for one workspace.
type Store struct {
	mu   sync.Mutex
	path string
	d    doc
}

// Open loads the metadata document. A missing file is an empty store; a
// malformed file refuses by name (never silently resets human state).
func Open(root string) (*Store, error) {
	s := &Store{path: filepath.Join(root, File), d: emptyDoc()}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channelmeta: read: %w", err)
	}
	if err := json.Unmarshal(data, &s.d); err != nil {
		return nil, fmt.Errorf("channelmeta: parse %s: %w", File, err)
	}
	if s.d.Schema != 1 {
		return nil, fmt.Errorf("channelmeta: schema %d, want 1", s.d.Schema)
	}
	if s.d.Reactions == nil {
		s.d.Reactions = map[string]map[string]int{}
	}
	if s.d.Edits == nil {
		s.d.Edits = map[string]string{}
	}
	if s.d.Pins == nil {
		s.d.Pins = map[string]bool{}
	}
	return s, nil
}

func emptyDoc() doc {
	return doc{Schema: 1,
		Reactions: map[string]map[string]int{},
		Edits:     map[string]string{},
		Pins:      map[string]bool{}}
}

// key encodes a channel + message id.
func key(channel string, id int64) string {
	return channel + "|" + strconv.FormatInt(id, 10)
}

// Reactions lists the reaction tokens with a positive count, sorted.
func (s *Store) Reactions(channel string, id int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.d.Reactions[key(channel, id)]
	out := make([]string, 0, len(m))
	for tok, n := range m {
		if n > 0 {
			out = append(out, tok)
		}
	}
	sort.Strings(out)
	return out
}

// ToggleReaction adds or removes one reaction of token by the human.
func (s *Store) ToggleReaction(channel string, id int64, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("channelmeta: empty reaction token")
	}
	k := key(channel, id)
	s.mu.Lock()
	m := s.d.Reactions[k]
	if m == nil {
		m = map[string]int{}
		s.d.Reactions[k] = m
	}
	if m[token] > 0 {
		delete(m, token)
	} else {
		m[token] = 1
	}
	if len(m) == 0 {
		delete(s.d.Reactions, k)
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// EditedText returns the replacement text for a message, if edited.
func (s *Store) EditedText(channel string, id int64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.d.Edits[key(channel, id)]
	return t, ok
}

// Edit replaces a message's display text (never the stored JSONL fact).
func (s *Store) Edit(channel string, id int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("channelmeta: edit text required")
	}
	s.mu.Lock()
	s.d.Edits[key(channel, id)] = text
	err := s.save()
	s.mu.Unlock()
	return err
}

// IsPinned reports whether a message is pinned.
func (s *Store) IsPinned(channel string, id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Pins[key(channel, id)]
}

// TogglePin pins or unpins a message.
func (s *Store) TogglePin(channel string, id int64) error {
	k := key(channel, id)
	s.mu.Lock()
	if s.d.Pins[k] {
		delete(s.d.Pins, k)
	} else {
		s.d.Pins[k] = true
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// Pinned lists the pinned message ids in a channel, ascending.
func (s *Store) Pinned(channel string) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := channel + "|"
	var out []int64
	for k, v := range s.d.Pins {
		if !v || !strings.HasPrefix(k, prefix) {
			continue
		}
		if id, err := strconv.ParseInt(strings.TrimPrefix(k, prefix), 10, 64); err == nil {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("channelmeta: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Dir is the reserved channels tree (re-exported for callers).
const Dir = workspace.DirChannels
