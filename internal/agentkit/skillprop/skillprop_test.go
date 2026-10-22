package skillprop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/install"
)

// skillRepo is a published repository with one Agent Skill and a script;
// fetch "clones" it and reports commit.
func skillRepo(t *testing.T, commit *string) (install.Source, install.Fetcher) {
	t.Helper()
	repo := t.TempDir()
	d := filepath.Join(repo, "skills", "tf")
	os.MkdirAll(filepath.Join(d, "scripts"), 0o755)
	os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: tf\ndescription: plan before apply. Use for terraform changes.\n---\n\nAlways run scripts/plan.sh first.\n"), 0o644)
	os.WriteFile(filepath.Join(d, "scripts", "plan.sh"), []byte("#!/bin/sh\nterraform plan\n"), 0o755)
	fetch := func(ctx context.Context, url, ref, dst string) (string, error) {
		if err := os.CopyFS(dst, os.DirFS(repo)); err != nil {
			return "", err
		}
		return *commit, nil
	}
	return install.Source{URL: "https://github.com/acme/skills.git"}, fetch
}

func scope(t *testing.T) install.Scope {
	root := t.TempDir()
	return install.Scope{Name: ScopeWorkspace, SkillsDir: filepath.Join(root, "skills"),
		MCPFile: filepath.Join(root, "mcp.json"), LockPath: filepath.Join(root, install.LockFile)}
}

// F-091: a suggestion records every skill and script in full, pinned;
// the approval installs exactly that commit with its scripts runnable.
func TestReviewThenInstall(t *testing.T) {
	commit := "c1"
	src, fetch := skillRepo(t, &commit)
	p, err := Review(context.Background(), fetch, t.TempDir(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Skills) != 1 || !p.HasScripts() || !strings.Contains(p.Skills[0].Scripts[0].Text, "terraform plan") {
		t.Fatalf("review: %+v", p)
	}
	if p.Version != "c1" || !strings.Contains(p.Entry, "#c1") || !strings.Contains(p.Trust(), "c1") {
		t.Fatalf("pinning: %+v", p)
	}
	sc := scope(t)
	names, err := Install(context.Background(), fetch, t.TempDir(), p, sc)
	if err != nil || len(names) != 1 || names[0] != "tf" {
		t.Fatalf("install = %v, %v", names, err)
	}
	if fi, err := os.Stat(filepath.Join(sc.SkillsDir, "tf", "scripts", "plan.sh")); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatal("a reviewed script must be runnable after approval")
	}
}

func TestInstallRefusesWhatWasNotReviewed(t *testing.T) {
	commit := "c1"
	src, fetch := skillRepo(t, &commit)
	p, err := Review(context.Background(), fetch, t.TempDir(), src)
	if err != nil {
		t.Fatal(err)
	}
	commit = "c2" // the repository moved on
	if _, err := Install(context.Background(), fetch, t.TempDir(), p, scope(t)); err == nil || !strings.Contains(err.Error(), "changed since it was reviewed") {
		t.Fatalf("err = %v", err)
	}
}

func TestReviewRefusesASourceWithoutSkills(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("no skills here"), 0o644)
	if _, err := Review(context.Background(), nil, t.TempDir(), install.Source{Path: dir}); err == nil {
		t.Fatal("a folder without SKILL.md is not a skill suggestion")
	}
}
