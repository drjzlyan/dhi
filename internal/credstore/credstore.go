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
	// Workspaces holds secrets saved for one workspace only (F-089),
	// keyed by its absolute root. They live here, never under .dhi/.
	Workspaces map[string]scoped `toml:"workspaces,omitempty"`
	// Settings are plain values a server needs that are not secrets (a
	// site URL, a username): resolved like credentials, never redacted
	// (F-104).
	Settings map[string]string `toml:"settings,omitempty"`
}

type scoped struct {
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
	d.Workspaces = back.Workspaces
	d.Settings = back.Settings
	return d, nil
}

// Secret is one stored secret's name and where it applies; never the
// value (F-089).
type Secret struct {
	Name      string
	Workspace string // absolute root; "" = every workspace
}

// LookupIn resolves name for the workspace at root: its own secret
// first, then the global one.
func (s *Store) LookupIn(root, name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return "", false
	}
	if v := d.Workspaces[root].Credentials[name]; v != "" {
		return v, true
	}
	if v := d.Credentials[name]; v != "" {
		return v, true
	}
	v, ok := d.Settings[name]
	return v, ok && v != ""
}

// SetSetting stores plain (non-secret) values: resolved like secrets,
// never redacted.
func (s *Store) SetSetting(values map[string]string) error {
	if s.Path == "" {
		return fmt.Errorf("credstore: no credentials file path")
	}
	for n := range values {
		if !nameRe.MatchString(n) {
			return fmt.Errorf("credstore: %q is not a variable name", n)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	if d.Settings == nil {
		d.Settings = map[string]string{}
	}
	for n, v := range values {
		d.Settings[n] = v
		delete(d.Credentials, n) // one name, one kind
	}
	return s.write(d)
}

// ReclassifySettings moves the named global credentials into Settings,
// so their values stop being redacted (the F-104 repair). It reports the
// names it moved.
func (s *Store) ReclassifySettings(names ...string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	var moved []string
	for _, n := range names {
		v, ok := d.Credentials[n]
		if !ok {
			continue
		}
		if d.Settings == nil {
			d.Settings = map[string]string{}
		}
		d.Settings[n] = v
		delete(d.Credentials, n)
		moved = append(moved, n)
	}
	if len(moved) == 0 {
		return nil, nil
	}
	return moved, s.write(d)
}

// SetIn stores one secret for the workspace at root ("" = every
// workspace), atomically.
func (s *Store) SetIn(root, name, value string) error {
	if root == "" {
		return s.Set(map[string]string{name: value})
	}
	if s.Path == "" {
		return fmt.Errorf("credstore: no credentials file path")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("credstore: %q is not a variable name", name)
	}
	if value == "" {
		return fmt.Errorf("credstore: %s is empty", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	if d.Workspaces == nil {
		d.Workspaces = map[string]scoped{}
	}
	sc := d.Workspaces[root]
	if sc.Credentials == nil {
		sc.Credentials = map[string]string{}
	}
	sc.Credentials[name] = value
	d.Workspaces[root] = sc
	return s.write(d)
}

// DeleteIn removes a workspace's secret ("" = the global one).
func (s *Store) DeleteIn(root, name string) error {
	if root == "" {
		return s.Delete(name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	if sc, ok := d.Workspaces[root]; ok {
		delete(sc.Credentials, name)
		if len(sc.Credentials) == 0 {
			delete(d.Workspaces, root)
		}
	}
	return s.write(d)
}

// Secrets lists what applies to the workspace at root — its own secrets
// and the global ones — by name, never values.
func (s *Store) Secrets(root string) ([]Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	var out []Secret
	for n := range d.Workspaces[root].Credentials {
		out = append(out, Secret{Name: n, Workspace: root})
	}
	for n := range d.Credentials {
		out = append(out, Secret{Name: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Workspace > out[j].Workspace // this workspace's first
	})
	return out, nil
}

// ValidName reports whether name can be a secret (an env var name).
func ValidName(name string) bool { return nameRe.MatchString(name) }

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

// ScopeSep separates a secret's name from its workspace in Values keys.
const ScopeSep = "\x00"

// Values returns every stored value by name, for redaction only (F-088):
// the redactor must know a value to keep it out of what DHI writes.
// Nothing else reads values in bulk.
func (s *Store) Values() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(d.Credentials))
	for n, v := range d.Credentials {
		out[n] = v
	}
	// Workspace secrets too: a value is redacted wherever it appears,
	// whichever workspace saved it. The key keeps two workspaces' values
	// of one name apart; the label is the name alone (ScopeSep).
	for root, sc := range d.Workspaces {
		for n, v := range sc.Credentials {
			out[n+ScopeSep+root] = v
		}
	}
	return out
}

// Stamp changes whenever the file does (size and mtime), so a cache of
// Values knows to reload.
func (s *Store) Stamp() string {
	if s.Path == "" {
		return ""
	}
	fi, err := os.Stat(s.Path)
	if err != nil {
		return "absent"
	}
	return fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())
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
