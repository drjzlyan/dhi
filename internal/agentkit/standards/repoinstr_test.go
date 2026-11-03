package standards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func repoFixture(t *testing.T) (root string, ms []workspace.Member) {
	t.Helper()
	root = t.TempDir()
	for name, body := range map[string]string{"api": "api rule", "web": "web rule", "docs": ""} {
		d := filepath.Join(root, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if err := os.WriteFile(filepath.Join(d, "AGENTS.md"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		ms = append(ms, workspace.Member{Name: name, Path: d})
	}
	return root, ms
}

func TestRepoInstructionsSkipsWhatTheCLIReads(t *testing.T) {
	root, ms := repoFixture(t)
	got := RepoInstructions(ms, nil, filepath.Join(root, "api"), true)
	if strings.Contains(got, "api rule") || !strings.Contains(got, "web/AGENTS.md:\nweb rule") {
		t.Fatalf("cwd member must be deduped, others carried:\n%s", got)
	}
	if strings.Contains(got, "docs/") {
		t.Fatalf("empty file listed:\n%s", got)
	}
}

func TestRepoInstructionsCarriesAllWhenCLIDoesNotRead(t *testing.T) {
	root, ms := repoFixture(t)
	got := RepoInstructions(ms, nil, filepath.Join(root, "api"), false)
	if !strings.Contains(got, "api rule") || !strings.Contains(got, "web rule") {
		t.Fatalf("both files expected:\n%s", got)
	}
}

func TestRepoInstructionsPrefersTaskWorktree(t *testing.T) {
	root, ms := repoFixture(t)
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, "AGENTS.md"), []byte("branch rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RepoInstructions(ms, map[string]string{"web": wt}, filepath.Join(root, "api"), true)
	if !strings.Contains(got, "branch rule") || strings.Contains(got, "web rule") {
		t.Fatalf("worktree copy must win:\n%s", got)
	}
}

func TestRepoInstructionsNothingToSay(t *testing.T) {
	root, ms := repoFixture(t)
	if got := RepoInstructions(ms[:1], nil, filepath.Join(root, ms[0].Name), true); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRepoInstructionsCaps(t *testing.T) {
	root, ms := repoFixture(t)
	big := strings.Repeat("x", repoFileRunes*2)
	if err := os.WriteFile(filepath.Join(root, "api", "AGENTS.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RepoInstructions(ms, nil, "", false)
	if !strings.Contains(got, "truncated; read") || len(got) > repoTotalRunes+1000 {
		t.Fatalf("cap not applied (%d bytes)", len(got))
	}
}
