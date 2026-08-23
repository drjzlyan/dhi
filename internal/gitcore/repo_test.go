package gitcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initRepo creates a repo with an initial commit and returns its root.
func initRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("hello.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@test", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	rp, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return rp, dir
}

func TestStatusUntrackedModifiedStaged(t *testing.T) {
	rp, dir := initRepo(t)

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644)         // untracked
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("changed\n"), 0o644) // modified

	st, err := rp.Status()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FileStatus{}
	for _, f := range st {
		byPath[f.Path] = f
	}
	if f := byPath["new.txt"]; f.Y != '?' && f.Y != 'U' {
		t.Errorf("untracked Y = %q", f.Y)
	}
	if f := byPath["hello.txt"]; f.Y != 'M' || f.Staged {
		t.Errorf("modified = %+v", f)
	}

	if err := rp.Stage("hello.txt"); err != nil {
		t.Fatal(err)
	}
	st, _ = rp.Status()
	byPath = map[string]FileStatus{}
	for _, f := range st {
		byPath[f.Path] = f
	}
	if f := byPath["hello.txt"]; !f.Staged {
		t.Errorf("staged flag after Stage: %+v", f)
	}
}

func TestStageAllAndCommit(t *testing.T) {
	rp, dir := initRepo(t)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("v2\n"), 0o644)

	if err := rp.Stage("."); err != nil {
		t.Fatal(err)
	}
	hash, err := rp.Commit(CommitOptions{Message: "second", Author: "dev", Email: "d@d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) < 7 {
		t.Fatalf("hash = %q", hash)
	}

	log, err := rp.Log(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Message != "second" || log[1].Message != "initial" {
		t.Fatalf("log = %+v", log)
	}

	st, _ := rp.Status()
	if len(st) != 0 {
		t.Errorf("worktree not clean after commit: %+v", st)
	}
}

func TestCommitEmptyMessageRefused(t *testing.T) {
	rp, _ := initRepo(t)
	if _, err := rp.Commit(CommitOptions{Message: "  "}); err == nil {
		t.Fatal("empty message accepted")
	}
}

func TestCommitNothingStagedRefused(t *testing.T) {
	rp, _ := initRepo(t)
	if _, err := rp.Commit(CommitOptions{Message: "x", Author: "a", Email: "b@c"}); err == nil {
		t.Fatal("commit without staged changes accepted")
	} else if !strings.Contains(err.Error(), "nothing staged") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnstageKeepsWorktree(t *testing.T) {
	rp, dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("v2\n"), 0o644)
	if err := rp.Stage("hello.txt"); err != nil {
		t.Fatal(err)
	}

	if err := rp.Unstage("hello.txt"); err != nil {
		t.Fatal(err)
	}

	st, err := rp.Status()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st {
		if f.Path == "hello.txt" && f.Staged {
			t.Fatalf("still staged after Unstage: %+v", f)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if string(data) != "v2\n" {
		t.Fatalf("worktree content lost: %q", data)
	}
}

func TestBranchAndNotARepo(t *testing.T) {
	rp, _ := initRepo(t)
	br, err := rp.CurrentBranch()
	if err != nil || br == "" {
		t.Errorf("branch = %q, %v", br, err)
	}
	if IsRepo(t.TempDir()) {
		t.Error("IsRepo true for plain dir")
	}
	if _, err := Open(""); err == nil {
		t.Error("empty path accepted")
	}
}
