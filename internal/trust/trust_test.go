package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUntrustedByDefault(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "trust.json"))
	if s.Trusted(t.TempDir()) {
		t.Fatal("a folder nobody trusted is trusted")
	}
}

func TestTrustPersistsAndCoversChildren(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "trust.json")
	repo := t.TempDir()
	child := filepath.Join(repo, "pkg", "x")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := New(path).Trust(repo); err != nil {
		t.Fatal(err)
	}
	s := New(path) // a second process sees it
	if !s.Trusted(repo) || !s.Trusted(child) {
		t.Fatal("trust did not persist or cover a child")
	}
	if s.Trusted(filepath.Dir(repo)) || s.Trusted(repo+"-sibling") {
		t.Fatal("trust leaked to the parent or a sibling with the same prefix")
	}
}

func TestUntrust(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "trust.json"))
	repo := t.TempDir()
	if err := s.Trust(repo); err != nil {
		t.Fatal(err)
	}
	if err := s.Untrust(repo); err != nil {
		t.Fatal(err)
	}
	if s.Trusted(repo) {
		t.Fatal("still trusted after untrust")
	}
}

func TestUntrustChildOfTrustedParentNamesTheParent(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "trust.json"))
	root := t.TempDir()
	child := filepath.Join(root, "a")
	_ = os.MkdirAll(child, 0o755)
	_ = s.Trust(root)
	err := s.Untrust(child)
	if err == nil || !strings.Contains(err.Error(), "untrust that folder") {
		t.Fatalf("want a parent-naming error, got %v", err)
	}
	if !s.Trusted(child) {
		t.Fatal("child lost trust")
	}
}

func TestCorruptRecordTrustsNothingAndSaysWhy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if s.Trusted(t.TempDir()) {
		t.Fatal("a corrupt record trusted a folder")
	}
	if s.Err() == nil {
		t.Fatal("Err hides the corrupt record")
	}
	if s.Trust(t.TempDir()) == nil {
		t.Fatal("Trust overwrote a corrupt record silently")
	}
}
