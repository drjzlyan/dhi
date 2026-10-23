package gitcore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// With the user's git set as the network git, clone, fetch and push honour
// their config — here an insteadOf rule go-git would ignore (F-112).
func TestNetworkUsesTheUsersGitConfig(t *testing.T) {
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	base := t.TempDir()
	gitcfg := filepath.Join(base, "gitconfig")
	origin := filepath.Join(base, "origin.git")
	os.WriteFile(gitcfg, []byte("[user]\n\tname = T\n\temail = t@example.com\n[init]\n\tdefaultBranch = main\n"+
		"[url \""+origin+"\"]\n\tinsteadOf = corp:team/repo\n"), 0o644)
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL="+gitcfg, "GIT_CONFIG_NOSYSTEM=1")
	r := NewRunner(bin, BackgroundEnv(env))
	ctx := context.Background()
	must := func(dir string, args ...string) string {
		t.Helper()
		out, _, err := r.Run(ctx, dir, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	must("", "init", "-q", "--bare", origin)
	seed := filepath.Join(base, "seed")
	must("", "clone", "-q", origin, seed)
	os.WriteFile(filepath.Join(seed, "a.txt"), []byte("a\n"), 0o644)
	must(seed, "add", "-A")
	must(seed, "commit", "-qm", "one")
	must(seed, "push", "-q", "origin", "HEAD:refs/heads/main")
	first := must(seed, "rev-parse", "HEAD")

	SetNetwork(r)
	t.Cleanup(func() { SetNetwork(nil) })

	dst := filepath.Join(base, "clone")
	repo, err := Clone(ctx, "corp:team/repo", dst)
	if err != nil {
		t.Fatalf("clone through insteadOf: %v", err)
	}
	os.WriteFile(filepath.Join(dst, "b.txt"), []byte("b\n"), 0o644)
	must(dst, "add", "-A")
	must(dst, "commit", "-qm", "two")
	if err := repo.Push(ctx, "", "refs/heads/main:refs/heads/feature"); err != nil {
		t.Fatalf("push: %v", err)
	}
	sr, _ := Open(seed)
	got, err := sr.Fetch(ctx, "", "+refs/heads/feature:refs/dhi/feature")
	if err != nil || got != must(dst, "rev-parse", "HEAD") {
		t.Fatalf("fetch = %q %v", got, err)
	}
	at, err := CloneAt(ctx, "corp:team/repo", first, filepath.Join(base, "pinned"))
	if err != nil || at != first {
		t.Fatalf("clone at %s = %q %v", first, at, err)
	}
	if _, err := Clone(ctx, "corp:nope", filepath.Join(base, "x")); err == nil {
		t.Fatal("a bad remote must fail, not prompt")
	}
}
