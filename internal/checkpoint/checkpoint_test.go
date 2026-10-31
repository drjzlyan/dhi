package checkpoint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitcore"
)

func setup(t *testing.T) (*Store, string) {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	_ = os.MkdirAll(repo, 0o755)
	git := gitcore.NewRunner(bin, append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1"))
	if _, _, err := git.Run(context.Background(), repo, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "keep.txt", "keep\n")
	write(t, repo, "edit.txt", "one\n")
	write(t, repo, ".gitignore", "build/\n")
	write(t, repo, "build/out.bin", "ignored\n")
	s := Open(root, git, func() []Member { return []Member{{Name: "api", Path: repo}} })
	return s, repo
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// F-120: undo a turn's edits, additions and deletions; the member's own
// .git gains nothing.
func TestTurnUndo(t *testing.T) {
	s, repo := setup(t)
	ctx := context.Background()
	refsBefore, _ := os.ReadDir(filepath.Join(repo, ".git", "refs", "heads"))
	before, err := s.Snapshot(ctx, "before")
	if err != nil {
		t.Fatal(err)
	}
	write(t, repo, "edit.txt", "two\n")
	write(t, repo, "new.txt", "new\n")
	_ = os.Remove(filepath.Join(repo, "keep.txt"))
	write(t, repo, "build/out.bin", "changed\n") // ignored: not part of the turn
	after, err := s.Snapshot(ctx, "after")
	if err != nil {
		t.Fatal(err)
	}
	turn := Turn{Agent: "ana", Before: before, After: after}
	cs, err := s.Diff(ctx, before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
		t.Fatalf("changes = %+v", cs)
	}
	if conf, _ := s.Conflicts(ctx, turn, cs); len(conf) != 0 {
		t.Fatalf("conflicts = %+v", conf)
	}
	if err := s.Restore(ctx, before, cs); err != nil {
		t.Fatal(err)
	}
	if read(t, repo, "edit.txt") != "one\n" || read(t, repo, "keep.txt") != "keep\n" || read(t, repo, "new.txt") != "<missing>" {
		t.Fatal("not restored")
	}
	refsAfter, _ := os.ReadDir(filepath.Join(repo, ".git", "refs", "heads"))
	if len(refsAfter) != len(refsBefore) {
		t.Fatal("the member's .git changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "refs", "dhi")); err == nil {
		t.Fatal("refs written into the member's repo")
	}
}

func TestConflictWhenEditedAfterTurn(t *testing.T) {
	s, repo := setup(t)
	ctx := context.Background()
	before, _ := s.Snapshot(ctx, "before")
	write(t, repo, "edit.txt", "agent\n")
	after, _ := s.Snapshot(ctx, "after")
	write(t, repo, "edit.txt", "human edit\n")
	cs, _ := s.Diff(ctx, before, after)
	conf, err := s.Conflicts(ctx, Turn{Before: before, After: after}, cs)
	if err != nil || len(conf) != 1 {
		t.Fatalf("conflicts = %+v %v", conf, err)
	}
}

func TestRecordPrunes(t *testing.T) {
	s, _ := setup(t)
	s.keep = 2
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		p, _ := s.Snapshot(ctx, "x")
		if err := s.Record(ctx, Turn{Agent: "a", Before: p, After: p}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(s.Turns()); n != 2 {
		t.Fatalf("turns = %d", n)
	}
}
