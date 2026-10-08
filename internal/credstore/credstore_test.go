package credstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Path: filepath.Join(t.TempDir(), "dhi", File)}
}

func TestSetLookupDeleteAndNamesNeverShowValues(t *testing.T) {
	s := newStore(t)
	if _, ok := s.Lookup("A"); ok {
		t.Fatal("empty store returned a value")
	}
	if err := s.Set(map[string]string{"JIRA_API_TOKEN": "s3cret", "JIRA_URL": "https://x.atlassian.net"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Lookup("JIRA_API_TOKEN"); !ok || v != "s3cret" {
		t.Fatalf("lookup = %q %v", v, ok)
	}
	names, _ := s.Names()
	if strings.Join(names, ",") != "JIRA_API_TOKEN,JIRA_URL" {
		t.Fatalf("names = %v", names)
	}
	// An update merges, it does not replace the file.
	if err := s.Set(map[string]string{"NOTION_TOKEN": "n"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Lookup("JIRA_URL"); !ok {
		t.Fatal("a second Set dropped earlier credentials")
	}
	if err := s.Delete("JIRA_API_TOKEN", "never-there"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Lookup("JIRA_API_TOKEN"); ok {
		t.Fatal("deleted credential still found")
	}
}

func TestFileAndDirectoryAreOwnerOnly(t *testing.T) {
	s := newStore(t)
	if err := s.Set(map[string]string{"X": "y"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v err=%v, want 0600", fi.Mode().Perm(), err)
	}
	di, _ := os.Stat(filepath.Dir(s.Path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", di.Mode().Perm())
	}
	// Re-writing keeps the mode (the temp file is created restrictive).
	if err := s.Set(map[string]string{"Z": "1"}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(s.Path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode after rewrite = %v", fi.Mode().Perm())
	}
	// No temp files are left behind.
	entries, _ := os.ReadDir(filepath.Dir(s.Path))
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestSetValidatesNamesAndValues(t *testing.T) {
	s := newStore(t)
	for _, bad := range []map[string]string{{"bad name": "x"}, {"1ABC": "x"}, {"OK": ""}} {
		if err := s.Set(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if _, err := os.Stat(s.Path); err == nil {
		t.Fatal("a rejected Set must not create the file")
	}
	if err := (&Store{}).Set(map[string]string{"A": "b"}); err == nil {
		t.Fatal("a path-less store accepted a write")
	}
}

func TestCorruptFileIsNamedNotSilentlyReplaced(t *testing.T) {
	s := newStore(t)
	os.MkdirAll(filepath.Dir(s.Path), 0o700)
	os.WriteFile(s.Path, []byte("{{ not toml"), 0o600)
	if _, ok := s.Lookup("A"); ok {
		t.Fatal("corrupt file produced a value")
	}
	err := s.Set(map[string]string{"A": "b"})
	if err == nil || !strings.Contains(err.Error(), s.Path) {
		t.Fatalf("Set over a corrupt file = %v; it must refuse and name the file", err)
	}
	if data, _ := os.ReadFile(s.Path); string(data) != "{{ not toml" {
		t.Fatal("the corrupt file was overwritten")
	}
}

func TestDefaultPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if p, _ := DefaultPath(); p != "/cfg/dhi/credentials.toml" {
		t.Fatalf("path = %q", p)
	}
}
