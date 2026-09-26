package dhitools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initRepo makes dir a repo with one commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("a.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGitToolsServed(t *testing.T) {
	for _, s := range []string{"git_status", "git_log", "git_branch"} {
		if !Serves(s) {
			t.Fatalf("%s is not a served slug", s)
		}
	}
}

func TestGitStatusCleanThenDirty(t *testing.T) {
	f, m := newFixture(t, "git_status")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "git_status", `{}`)
	if isErr {
		t.Fatalf("git_status refused: %s", out)
	}
	if !strings.Contains(out, "clean") {
		t.Fatalf("clean status = %q", out)
	}

	writeFile(t, api, "a.go", "package a // changed\n")
	out, isErr = call(h, t, "git_status", `{}`)
	if isErr || !strings.Contains(out, "a.go") {
		t.Fatalf("dirty status = %q err=%v", out, isErr)
	}
}

func TestGitLogListsCommits(t *testing.T) {
	f, m := newFixture(t, "git_log")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "git_log", `{}`)
	if isErr || !strings.Contains(out, "initial commit") {
		t.Fatalf("git_log = %q err=%v", out, isErr)
	}
}

func TestGitBranchReportsCurrent(t *testing.T) {
	f, m := newFixture(t, "git_branch")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "git_branch", `{}`)
	if isErr || !strings.Contains(out, "current:") {
		t.Fatalf("git_branch = %q err=%v", out, isErr)
	}
}

func TestGitToolsRefuseWithoutRepo(t *testing.T) {
	f, m := newFixture(t, "git_status")
	h := Deps{Agent: m, WS: f.ws, Workdir: memberDir(t, f.ws, "api"), Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "git_status", `{}`)
	if !isErr || !strings.Contains(out, "not a git repository") {
		t.Fatalf("non-repo = %q isErr=%v", out, isErr)
	}
}
