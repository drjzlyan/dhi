package channelmeta

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReactionsEditsPinsRoundTrip(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Reaction toggle add/remove.
	if err := s.ToggleReaction("#general", 7, "+1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ToggleReaction("#general", 7, "ok"); err != nil {
		t.Fatal(err)
	}
	if got := s.Reactions("#general", 7); len(got) != 2 || got[0] != "+1" || got[1] != "ok" {
		t.Fatalf("reactions = %v", got)
	}
	if err := s.ToggleReaction("#general", 7, "+1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Reactions("#general", 7); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("after untoggle = %v", got)
	}
	// Edit.
	if err := s.Edit("#general", 7, "corrected"); err != nil {
		t.Fatal(err)
	}
	if txt, ok := s.EditedText("#general", 7); !ok || txt != "corrected" {
		t.Fatalf("edit = %q ok=%v", txt, ok)
	}
	if err := s.Edit("#general", 7, "  "); err == nil {
		t.Error("empty edit accepted")
	}
	// Pins.
	if err := s.TogglePin("#general", 7); err != nil {
		t.Fatal(err)
	}
	if err := s.TogglePin("#general", 9); err != nil {
		t.Fatal(err)
	}
	if !s.IsPinned("#general", 7) || s.IsPinned("#general", 8) {
		t.Fatal("pin state wrong")
	}
	if got := s.Pinned("#general"); len(got) != 2 || got[0] != 7 || got[1] != 9 {
		t.Fatalf("pinned = %v", got)
	}
	// Other channels are isolated.
	if len(s.Pinned("#other")) != 0 {
		t.Fatal("pins leaked across channels")
	}
	// Round-trip through disk.
	s2, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Reactions("#general", 7); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("reload reactions = %v", got)
	}
	if txt, _ := s2.EditedText("#general", 7); txt != "corrected" {
		t.Fatalf("reload edit = %q", txt)
	}
	if !s2.IsPinned("#general", 7) {
		t.Fatal("reload lost pin")
	}
}

func TestOpenRefusesMalformed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, File), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("malformed metadata accepted")
	}
	// Missing file is an empty store.
	if s, err := Open(t.TempDir()); err != nil || len(s.Reactions("#x", 1)) != 0 {
		t.Fatalf("empty open = %v %v", s, err)
	}
}
