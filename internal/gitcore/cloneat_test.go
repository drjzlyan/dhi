package gitcore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// CloneAt checks out the pinned commit, and the default branch without one.
func TestCloneAtPinsTheCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH for the fixture")
	}
	src := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@b", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@b")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(src, "f"), []byte("one"), 0o644)
	run("add", ".")
	run("commit", "-qm", "one")
	first := run("rev-parse", "HEAD")[:40]
	os.WriteFile(filepath.Join(src, "f"), []byte("two"), 0o644)
	run("commit", "-qam", "two")

	dst := filepath.Join(t.TempDir(), "c")
	got, err := CloneAt(context.Background(), src, first, dst)
	if err != nil || got != first {
		t.Fatalf("pinned clone = %q, %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "f")); string(b) != "one" {
		t.Fatalf("checked out %q", b)
	}
	dst2 := filepath.Join(t.TempDir(), "c2")
	if _, err := CloneAt(context.Background(), src, "", dst2); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst2, "f")); string(b) != "two" {
		t.Fatalf("default branch %q", b)
	}
}
