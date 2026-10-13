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
