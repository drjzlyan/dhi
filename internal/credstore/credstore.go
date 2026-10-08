// Package credstore keeps integration credentials (F-047, ADR-0028) in one
// user-level file: <config dir>/dhi/credentials.toml, directory 0700, file
// 0600, written atomically. It is plaintext — like ~/.aws/credentials — and
// the UI says so. MCP cards hold credential *names* only; values live here
// (or in the environment, or on macOS the keychain), never under .dhi/.
package credstore

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"github.com/BurntSushi/toml"
)

// File is the credentials file name inside the dhi config dir.
const File = "credentials.toml"

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type document struct {
	Schema      int               `toml:"schema"`
	Credentials map[string]string `toml:"credentials"`
}

// DefaultPath is $XDG_CONFIG_HOME/dhi/credentials.toml (or ~/.config/...),
// next to the user's config.toml.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("credstore: locate home: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dhi", File), nil
}

// Store is a credentials file. The zero Path means "no file".
type Store struct {
	Path string
	mu   sync.Mutex
}

// Open returns the store at the default path.
func Open() (*Store, error) {
	p, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Store{Path: p}, nil
}

func (s *Store) load() (document, error) {
	d := document{Schema: 1, Credentials: map[string]string{}}
	if s.Path == "" {
		return d, nil
	}
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("credstore: read %s: %w", s.Path, err)
	}
	var back document
	if _, err := toml.Decode(string(data), &back); err != nil {
		return d, fmt.Errorf("credstore: %s is not valid TOML (fix or remove it): %w", s.Path, err)
	}
	if back.Schema != 1 {
		return d, fmt.Errorf("credstore: %s: schema %d, want 1", s.Path, back.Schema)
	}
	if back.Credentials != nil {
		d.Credentials = back.Credentials
	}
	return d, nil
}

// Lookup returns a stored credential. A corrupt file reads as not found
// here (the caller falls through to the next source); Set and Names report
// the corruption.
func (s *Store) Lookup(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return "", false
	}
	v, ok := d.Credentials[name]
	return v, ok && v != ""
}

// Names lists the stored credential names, sorted — never the values.
func (s *Store) Names() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(d.Credentials))
	for n := range d.Credentials {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// Set writes one or more credentials in a single atomic update.
func (s *Store) Set(values map[string]string) error {
	if s.Path == "" {
		return fmt.Errorf("credstore: no credentials file path")
	}
	for n, v := range values {
		if !nameRe.MatchString(n) {
			return fmt.Errorf("credstore: %q is not a variable name", n)
		}
		if v == "" {
			return fmt.Errorf("credstore: %s is empty", n)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	for n, v := range values {
		d.Credentials[n] = v
	}
	return s.write(d)
}

// Delete removes credentials; absent names are fine.
func (s *Store) Delete(names ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	for _, n := range names {
		delete(d.Credentials, n)
	}
	return s.write(d)
}

func (s *Store) write(d document) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("credstore: %w", err)
	}
	// MkdirAll leaves an existing dir's mode alone; tighten the one we own.
	if filepath.Base(dir) == "dhi" {
		_ = os.Chmod(dir, 0o700)
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*")
	if err != nil {
		return fmt.Errorf("credstore: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // no-op after a successful rename
	if err := os.Chmod(name, 0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("credstore: %w", err)
	}
	if err := toml.NewEncoder(tmp).Encode(d); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("credstore: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("credstore: %w", err)
	}
	if err := os.Rename(name, s.Path); err != nil {
		return fmt.Errorf("credstore: %w", err)
	}
	return nil
}
