package gitcore

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Live (M32): DHI's git_commit in a task worktree (a linked worktree,
// whose .git is a file) staged the change and then failed to commit, so
// agents fell back to their own git or left the fix staged.
func TestCommitInALinkedWorktree(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command(git, append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644)
	run(repo, "init", "-q", "-b", "main")
	run(repo, "add", "a.go")
	run(repo, "commit", "-qm", "init")
	wt := filepath.Join(t.TempDir(), "wt")
	run(repo, "worktree", "add", "-q", "-b", "task/x", wt)
	os.WriteFile(filepath.Join(wt, "a.go"), []byte("package a // fixed\n"), 0o644)

	r, err := Open(wt)
	if err != nil {
		t.Fatalf("open worktree: %v", err)
	}
	if err := r.Stage("."); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := r.Commit(CommitOptions{Message: "fix", Author: "t", Email: "t@t"}); err != nil {
		t.Fatalf("commit in a linked worktree: %v", err)
	}
	out, _ := exec.Command(git, "-C", wt, "log", "-1", "--format=%s", "task/x").CombinedOutput()
	if string(out) != "fix\n" {
		t.Fatalf("task/x head = %q", out)
	}
	main, _ := exec.Command(git, "-C", repo, "log", "-1", "--format=%s", "main").CombinedOutput()
	if string(main) != "init\n" {
		t.Fatalf("main moved: %q", main)
	}
}
