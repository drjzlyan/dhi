// Package trust records which folders the user has trusted to run things
// a repo can influence: language servers, formatters, tasks, tests and the
// debugger (F-148, G-S1). The record is DHI's own user-level state, never
// a file inside a repo, so a repo cannot trust itself.
package trust

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/drjzlyan/dhi/internal/settings"
)

// File is the record's name, next to the user config.
const File = "trust.json"

// DefaultPath is trust.json beside config.toml in the user's DHI folder.
func DefaultPath() (string, error) {
	p, err := settings.DefaultUserPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), File), nil
}

// Store is the set of trusted folders. A folder is trusted when it, or any
// folder above it, is in the set. The file is re-read on every question so
// `dhi doctor` and a running IDE agree.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns a Store backed by path.
func New(path string) *Store { return &Store{path: path} }

type record struct {
	Trusted []string `json:"trusted"`
}

// canon resolves symlinks where it can so one folder has one name.
func canon(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return dir
}

func (s *Store) read() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("trust: read %s: %w", s.path, err)
	}
	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("trust: %s is not valid: %w", s.path, err)
	}
	return r.Trusted, nil
}

func (s *Store) write(list []string) error {
	sort.Strings(list)
	data, err := json.MarshalIndent(record{Trusted: list}, "", "  ")
	if err != nil {
		return fmt.Errorf("trust: encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("trust: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("trust: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("trust: %w", err)
	}
	return nil
}

func covers(parent, dir string) bool {
	if parent == dir {
		return true
	}
	return strings.HasPrefix(dir, strings.TrimSuffix(parent, string(filepath.Separator))+string(filepath.Separator))
}

// Covering returns the trusted folder that covers dir, or "".
func (s *Store) Covering(dir string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return "" // an unreadable record trusts nothing; Err says why
	}
	d := canon(dir)
	best := ""
	for _, t := range list {
		if covers(t, d) && len(t) > len(best) {
			best = t
		}
	}
	return best
}

// Trusted reports whether dir is trusted.
func (s *Store) Trusted(dir string) bool { return s.Covering(dir) != "" }

// Err reports why the record cannot be read (nil when absent or fine).
func (s *Store) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.read()
	return err
}

// Trust adds dir.
func (s *Store) Trust(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return err
	}
	d := canon(dir)
	for _, t := range list {
		if t == d {
			return nil
		}
	}
	return s.write(append(list, d))
}

// Untrust removes dir. A folder trusted only through a parent cannot be
// untrusted on its own: the error names the parent to untrust instead.
func (s *Store) Untrust(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return err
	}
	d := canon(dir)
	var out []string
	found := false
	for _, t := range list {
		if t == d {
			found = true
			continue
		}
		out = append(out, t)
	}
	if !found {
		for _, t := range out {
			if covers(t, d) {
				return fmt.Errorf("trust: %s is trusted through %s — untrust that folder", d, t)
			}
		}
		return nil
	}
	for _, t := range out {
		if covers(t, d) {
			return s.write(out) // still covered by a parent; say so via Covering
		}
	}
	return s.write(out)
}

// List returns the trusted folders.
func (s *Store) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	sort.Strings(list)
	return list, err
}
