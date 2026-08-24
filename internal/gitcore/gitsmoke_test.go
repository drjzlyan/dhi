package gitcore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/toolchain"
)

// TestHermeticGitRoundTrip drives the real hermetic git shim through
// the operations M4 ChangeSets depend on (ADR-0009): init → commit →
// worktree add/list/remove/prune → local clone. Skipped unless:
//
//	DHI_SMOKE_GIT=1 [DHI_SMOKE_GIT_BIN=/path/to/git]
//
// DHI_SMOKE_GIT_BIN defaults to the installed toolchain shim. Build one
// locally with scripts/build-hermetic-git.sh.
func TestHermeticGitRoundTrip(t *testing.T) {
	if os.Getenv("DHI_SMOKE_GIT") != "1" {
		t.Skip("set DHI_SMOKE_GIT=1 to exercise the hermetic git CLI end-to-end")
	}
	bin := os.Getenv("DHI_SMOKE_GIT_BIN")
	if bin == "" {
		mgr, err := toolchain.DefaultRoot()
		if err != nil {
			t.Fatalf("locate toolchain root: %v", err)
		}
		bin = toolchain.New(mgr).GitBin()
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("hermetic git not found at %s (build via scripts/build-hermetic-git.sh)", bin)
	}

	mgr := toolchain.New(filepath.Dir(filepath.Dir(bin))) // prefix = parent of bin/
	if err := mgr.EnsureGitConfig(); err != nil {
		t.Fatalf("EnsureGitConfig: %v", err)
	}
	r := NewRunner(bin, mgr.GitEnv(nil))
	ctx := context.Background()

	v, err := r.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	t.Logf("hermetic git %s at %s", v, bin)

	base := t.TempDir()
	repo := filepath.Join(base, "origin")
	if _, _, err := r.Run(ctx, "", "init", "-q", repo); err != nil {
		t.Fatalf("init: %v", err)
	}
	write := func(root, name, content string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(repo, "README.md", "# smoke\n")
	if _, _, err := r.Run(ctx, repo, "-c", "user.name=dhi-smoke", "-c",
		"user.email=smoke@dhi.local", "add", "-A"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := r.Run(ctx, repo, "-c", "user.name=dhi-smoke", "-c",
		"user.email=smoke@dhi.local", "commit", "-q", "-m", "seed"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Worktree lifecycle.
	wt := filepath.Join(base, "task-42")
	if err := r.WorktreeAdd(ctx, repo, wt, "task/42", ""); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(wt, "README.md")); err != nil ||
		strings.TrimSpace(string(data)) != "# smoke" {
		t.Fatalf("worktree checkout wrong: %v %q", err, data)
	}
	wts, err := r.WorktreeList(ctx, repo)
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(wts) != 2 {
		t.Fatalf("worktree count %d, want 2: %+v", len(wts), wts)
	}
	var linked *Worktree
	// git reports fully-resolved paths; macOS /var → /private/var makes
	// t.TempDir() paths differ from git's output.
	resolved, err := filepath.EvalSymlinks(wt)
	if err != nil {
		t.Fatalf("resolve %s: %v", wt, err)
	}
	for i := range wts {
		if wts[i].Path == resolved {
			linked = &wts[i]
		}
	}
	if linked == nil || linked.Branch != "task/42" {
		t.Fatalf("linked worktree missing or misparsed: %+v", wts)
	}
	write(wt, "src/main.go", "package main\n")
	if _, _, err := r.Run(ctx, wt, "-c", "user.name=dhi-smoke", "-c",
		"user.email=smoke@dhi.local", "add", "-A"); err != nil {
		t.Fatalf("add in worktree: %v", err)
	}
	if _, _, err := r.Run(ctx, wt, "-c", "user.name=dhi-smoke", "-c",
		"user.email=smoke@dhi.local", "commit", "-qm", "wip in worktree"); err != nil {
		t.Fatalf("commit in worktree: %v", err)
	}
	if err := r.WorktreeRemove(ctx, repo, wt, false); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if err := r.Prune(ctx, repo); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if wts, _ := r.WorktreeList(ctx, repo); len(wts) != 1 {
		t.Fatalf("post-prune worktrees = %+v, want only main", wts)
	}

	// Local clone (plain path → local transport, no network helpers).
	cloneDir := filepath.Join(base, "clone")
	if _, _, err := r.Run(ctx, "", "clone", "-q", repo, cloneDir); err != nil {
		t.Fatalf("local clone: %v", err)
	}
	headOf := func(dir string) string {
		out, _, err := r.Run(ctx, dir, "rev-parse", "--short", "HEAD")
		if err != nil {
			t.Fatalf("rev-parse %s: %v", dir, err)
		}
		return strings.TrimSpace(out)
	}
	if headOf(repo) == "" || headOf(repo) != headOf(cloneDir) {
		t.Errorf("clone HEAD %q does not match origin %q", headOf(cloneDir), headOf(repo))
	}
}
