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
