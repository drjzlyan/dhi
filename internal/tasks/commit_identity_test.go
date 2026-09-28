package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	"github.com/drjzlyan/dhi/internal/gitcore"
)

// commitFixture seeds one task with a real worktree repo at ws/wt.
func commitFixture(t *testing.T) (*Store, string) {
	t.Helper()
	s, ws := setupStore(t)
	if err := s.Create("feat", "Feat", "you", ""); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(ws.Root, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(wt, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.SetAttach(func(string, string, string, string) (string, error) { return "", nil },
		func(string, string) error { return nil })
	if err := s.RecordChangeSet("feat", ChangeSet{Member: "main", Branch: "task/feat", Path: "wt"}); err != nil {
		t.Fatal(err)
	}
	return s, wt
}

func TestCommitUsesResolvedIdentity(t *testing.T) {
	s, wt := commitFixture(t)
	called := false
	s.SetIdentity(func(context.Context) (gitcore.Identity, error) {
		called = true
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	})
	if err := s.Commit("feat", "F-029 commit"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !called {
		t.Fatal("identity resolver was not called")
	}
	repo, err := git.PlainOpen(wt)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	c, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if c.Author.Name != "Ada" || c.Author.Email != "ada@example.com" {
		t.Fatalf("author = %s <%s>, want the resolved identity", c.Author.Name, c.Author.Email)
	}
}

func TestCommitRefusesWithoutIdentity(t *testing.T) {
	s, _ := commitFixture(t)
	err := s.Commit("feat", "no identity")
	if !errors.Is(err, gitcore.ErrIdentityUnset) {
		t.Fatalf("err = %v, want ErrIdentityUnset", err)
	}
	if !strings.Contains(err.Error(), "user.name") {
		t.Fatalf("refusal must name the fix: %v", err)
	}
}

func seedRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(path, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitCoversAllChangesets(t *testing.T) {
	s, ws := setupStore(t)
	if err := s.Create("feat2", "Feat2", "you", ""); err != nil {
		t.Fatal(err)
	}
	seedRepo(t, filepath.Join(ws.Root, "a"))
	seedRepo(t, filepath.Join(ws.Root, "b"))
	s.SetAttach(func(string, string, string, string) (string, error) { return "", nil },
		func(string, string) error { return nil })
	_ = s.RecordChangeSet("feat2", ChangeSet{Member: "api", Branch: "task/feat2", Path: "a"})
	_ = s.RecordChangeSet("feat2", ChangeSet{Member: "web", Branch: "task/feat2", Path: "b"})
	s.SetIdentity(func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	})
	if err := s.Commit("feat2", "multi-member"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	for _, p := range []string{"a", "b"} {
		repo, err := git.PlainOpen(filepath.Join(ws.Root, p))
		if err != nil {
			t.Fatal(err)
		}
		head, err := repo.Head()
		if err != nil {
			t.Fatalf("%s: no commit: %v", p, err)
		}
		c, _ := repo.CommitObject(head.Hash())
		if c.Message != "multi-member" {
			t.Fatalf("%s commit = %q", p, c.Message)
		}
	}
}

func TestCommitNamesMemberFailure(t *testing.T) {
	s, ws := setupStore(t)
	if err := s.Create("feat3", "Feat3", "you", ""); err != nil {
		t.Fatal(err)
	}
	seedRepo(t, filepath.Join(ws.Root, "ok"))
	s.SetAttach(func(string, string, string, string) (string, error) { return "", nil },
		func(string, string) error { return nil })
	_ = s.RecordChangeSet("feat3", ChangeSet{Member: "api", Branch: "b", Path: "ok"})
	_ = s.RecordChangeSet("feat3", ChangeSet{Member: "web", Branch: "b", Path: "missing"})
	s.SetIdentity(func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	})
	err := s.Commit("feat3", "partial")
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("member failure must be named: %v", err)
	}
	// The member that could commit still did (no silent rollback).
	repo, _ := git.PlainOpen(filepath.Join(ws.Root, "ok"))
	if _, herr := repo.Head(); herr != nil {
		t.Fatalf("good member should have committed: %v", herr)
	}
}
