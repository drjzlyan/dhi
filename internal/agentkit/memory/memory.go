// Package memory implements per-agent private memory (ADR-0007): an
// append-only journal.jsonl of structured events plus a hand/model
// editable notes.md, stored under .dhi/memory/agents/<id>/.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
}

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
	return jsonl.Append(filepath.Join(dir, "journal.jsonl"), Entry{At: time.Now(), Kind: kind, Text: text})
}

// Journal returns up to limit most recent entries (oldest first).
func (s *Store) Journal(agentID string, limit int) ([]Entry, error) {
	dir, err := s.dir(agentID)
	if err != nil {
		return nil, err
	}
	all, err := jsonl.ReadAll[Entry](filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit >= len(all) {
		return all, nil
	}
	return all[len(all)-limit:], nil
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

// WriteNotes replaces notes.md atomically-ish (write + rename).
func (s *Store) WriteNotes(agentID, content string) error {
	dir, err := s.dir(agentID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}
	tmp := filepath.Join(dir, ".notes.tmp")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memory: write notes: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "notes.md")); err != nil {
		return fmt.Errorf("memory: rename notes: %w", err)
	}
	return nil
}
