package gitcore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func stashRepo(t *testing.T) (*Runner, string) {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
	r := NewRunner(bin, BackgroundEnv(env))
	if _, _, err := r.Run(context.Background(), dir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-qm", "one"}} {
		if _, _, err := r.Run(context.Background(), dir, a...); err != nil {
			t.Fatal(err)
		}
	}
	return r, dir
}

// F-129: stash, list, apply, pop and drop through the user's git.
func TestStashLifecycle(t *testing.T) {
	r, dir := stashRepo(t)
	ctx := context.Background()
	file := filepath.Join(dir, "a.txt")
	read := func() string { b, _ := os.ReadFile(file); return string(b) }

	if err := StashPush(ctx, r, dir, "nothing"); !errors.Is(err, ErrNothingToStash) {
		t.Fatalf("clean tree: %v", err)
	}
	_ = os.WriteFile(file, []byte("two\n"), 0o644)
	if err := StashPush(ctx, r, dir, "my wip"); err != nil {
		t.Fatal(err)
	}
	if read() != "one\n" {
		t.Fatalf("tree not reset: %q", read())
	}
	list, err := Stashes(ctx, r, dir)
	if err != nil || len(list) != 1 || list[0].Ref != "stash@{0}" || !strings.Contains(list[0].Message, "my wip") {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := StashApply(ctx, r, dir, "stash@{0}"); err != nil || read() != "two\n" {
		t.Fatalf("apply: %v %q", err, read())
	}
	if l, _ := Stashes(ctx, r, dir); len(l) != 1 {
		t.Fatal("apply must keep the stash")
	}
	_ = os.WriteFile(file, []byte("one\n"), 0o644)
	if err := StashPop(ctx, r, dir, "stash@{0}"); err != nil || read() != "two\n" {
		t.Fatalf("pop: %v %q", err, read())
	}
	if l, _ := Stashes(ctx, r, dir); len(l) != 0 {
		t.Fatal("pop must drop the stash")
	}

	_ = os.WriteFile(file, []byte("three\n"), 0o644)
	if err := StashPush(ctx, r, dir, ""); err != nil {
		t.Fatal(err)
	}
	if err := StashDrop(ctx, r, dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if l, _ := Stashes(ctx, r, dir); len(l) != 0 {
		t.Fatal("drop must remove it")
	}
	if err := StashDrop(ctx, r, dir, "HEAD"); err == nil {
		t.Fatal("a non-stash ref must be refused")
	}
}
