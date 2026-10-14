package gitcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBranchRefsIncludesOriginHead(t *testing.T) {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git", "refs", "remotes", "origin"), 0o755)
	os.WriteFile(filepath.Join(repo, ".git", "refs", "remotes", "origin", "HEAD"), []byte("ref: refs/remotes/origin/develop\n"), 0o644)
	got := strings.Join(DefaultBranchRefs(repo), " ")
	for _, want := range []string{"refs/heads/main", "refs/heads/main.lock", "refs/heads/master", "refs/heads/develop.lock"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	linked := t.TempDir()
	os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /x\n"), 0o644)
	if DefaultBranchRefs(linked) != nil {
		t.Fatal("a linked worktree has no refs of its own")
	}
}

func TestLooseDefaultRefsMaterializesAPackedMain(t *testing.T) {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git", "refs", "heads"), 0o755)
	sha := strings.Repeat("a", 40)
	os.WriteFile(filepath.Join(repo, ".git", "packed-refs"), []byte("# pack-refs with: peeled fully-peeled sorted\n"+sha+" refs/heads/main\n"), 0o644)
	LooseDefaultRefs(repo)
	b, err := os.ReadFile(filepath.Join(repo, ".git", "refs", "heads", "main"))
	if err != nil || strings.TrimSpace(string(b)) != sha {
		t.Fatalf("loose main = %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "refs", "heads", "master")); err == nil {
		t.Fatal("wrote a ref that does not exist")
	}
}

// Live (M32): ~/.gitconfig linked to a missing dotfiles file made every
// identity write fail with git's "could not lock config file".
func TestDanglingGlobalConfigIsNamed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if danglingGlobalConfig() != "" {
		t.Fatal("no link, no message")
	}
	if err := os.Symlink(filepath.Join(home, "dotfiles", ".gitconfig"), filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	if msg := danglingGlobalConfig(); !strings.Contains(msg, "does not exist") {
		t.Fatalf("msg = %q", msg)
	}
}
