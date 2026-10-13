// Package memory implements per-agent private memory (ADR-0007): an
// append-only journal.jsonl of structured events plus a hand/model
// editable notes.md, stored under .dhi/memory/agents/<id>/.
package memory

import (
	"fmt"
	"github.com/drjzlyan/dhi/internal/fsx"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/drjzlyan/dhi/internal/jsonl"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Entry is one journal record.
type Entry struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"` // e.g. "turn", "lesson", "preference"
	Text string    `json:"text"`
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Store roots private memory at ws's .dhi/memory tree.
type Store struct {
	root string // .dhi/memory
	// journalMax keeps each journal bounded (F-078); 0 = no rotation.
	journalMax atomic.Int32
}

// SetJournalMax sets how many entries a journal keeps before older ones
// roll into the monthly archive and the digest (0 = never rotate).
func (s *Store) SetJournalMax(n int) { s.journalMax.Store(int32(max(n, 0))) }

// Open prepares the store; the directory is created lazily on write.
func Open(ws *workspace.Workspace) *Store {
	return &Store{root: filepath.Join(ws.Root, workspace.DirMemory)}
}

func (s *Store) dir(agentID string) (string, error) {
	if !idRe.MatchString(agentID) {
		return "", fmt.Errorf("memory: bad agent id %q", agentID)
	}
	return filepath.Join(s.root, "agents", agentID), nil
}

// Append records one journal entry with the current timestamp.
func (s *Store) Append(agentID, kind, text string) error {
	dir, err := s.dir(agentID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(text) == "" {
		return fmt.Errorf("memory: kind and text are required")
	}
	path := filepath.Join(dir, "journal.jsonl")
	if err := jsonl.Append(path, Entry{At: time.Now(), Kind: kind, Text: text}); err != nil {
		return err
	}
	return s.rotate(dir, path)
}

// rotate keeps the journal at journalMax entries: when it grows a quarter
// past the cap, the oldest entries move to journal-<yyyymm>.jsonl and a
// line each joins digest.md — the agent's long memory in brief, newest
// last, itself capped (F-078).
func (s *Store) rotate(dir, path string) error {
	maxN := int(s.journalMax.Load())
	if maxN <= 0 {
		return nil
	}
	if st, err := os.Stat(path); err != nil || st.Size() < int64(maxN)*40 {
		return nil // cheap pre-check: too small to be over the cap
	}
	n := 0
	_ = jsonl.Read(path, func([]byte) error { n++; return nil })
	if n <= maxN+maxN/4 {
		return nil
	}
	old, err := jsonl.Compact[Entry](path, maxN)
	if err != nil || len(old) == 0 {
		return err
	}
	var digest strings.Builder
	for _, e := range old {
		if err := jsonl.Append(filepath.Join(dir, "journal-"+e.At.Format("200601")+".jsonl"), e); err != nil {
			return err
		}
		fmt.Fprintf(&digest, "- %s %s: %s\n", e.At.Format("2006-01-02"), e.Kind, clipRunes(oneLine(e.Text), 160))
	}
	return s.appendDigest(dir, digest.String())
}

// digestMax bounds digest.md; the oldest lines fall off the top.
const digestMax = 16 << 10

func (s *Store) appendDigest(dir, lines string) error {
	p := filepath.Join(dir, "digest.md")
	return fsx.Locked(p, func() error {
		cur, _ := os.ReadFile(p)
		next := string(cur) + lines
		for len(next) > digestMax {
			i := strings.IndexByte(next, '\n')
			if i < 0 {
				next = next[len(next)-digestMax:]
				break
			}
			next = next[i+1:]
		}
		return fsx.WriteAtomic(p, []byte(next), 0o644)
	})
}

// Digest is the agent's rolled-up journal history ("" when none).
func (s *Store) Digest(agentID string) (string, error) {
	dir, err := s.dir(agentID)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "digest.md"))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(b), err
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Journal returns up to limit most recent entries (oldest first).
func (s *Store) Journal(agentID string, limit int) ([]Entry, error) {
	dir, err := s.dir(agentID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "journal.jsonl")
	if limit > 0 {
		// The newest entries only, read from the end (F-078): a journal
		// grows for the life of the agent; a prompt needs a few lines.
		return jsonl.Tail[Entry](path, limit)
	}
	all, _, err := jsonl.ReadTolerant[Entry](path)
	return all, err
}

// ReadNotes returns the contents of notes.md ("" when absent).
func (s *Store) ReadNotes(agentID string) (string, error) {
	dir, err := s.dir(agentID)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("memory: read notes: %w", err)
	}
	return string(b), nil
}

// WriteNotes replaces notes.md atomically and durably (F-078).
func (s *Store) WriteNotes(agentID, content string) error {
	dir, err := s.dir(agentID)
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(filepath.Join(dir, "notes.md"), []byte(content), 0o644); err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	return nil
}
