package dhitools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/gitcore"
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

// fakeGitCLI is a scripted GitCLI for the diff tool.
type fakeGitCLI struct {
	calls [][]string
	out   string
	err   error
}

func (f *fakeGitCLI) Run(_ context.Context, dir string, a ...string) (string, string, error) {
	f.calls = append(f.calls, append([]string{dir}, a...))
	return f.out, "", f.err
}

func TestGitDiffServed(t *testing.T) {
	if !Serves("git_diff") {
		t.Fatal("git_diff is not a served slug")
	}
}

func TestGitDiffUnstagedAndStaged(t *testing.T) {
	f, m := newFixture(t, "git_diff")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	fake := &fakeGitCLI{out: "diff --git a/a.go b/a.go\n-a\n+b\n"}
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Git: fake, Approvals: f.approvals}.Handler()

	out, isErr := call(h, t, "git_diff", `{}`)
	if isErr || !strings.Contains(out, "+b") {
		t.Fatalf("git_diff = %q err=%v", out, isErr)
	}
	if got := strings.Join(fake.calls[0], " "); !strings.Contains(got, "diff") {
		t.Fatalf("args = %q", got)
	}

	if _, isErr := call(h, t, "git_diff", `{"staged":true}`); isErr {
		t.Fatal("staged diff refused")
	}
	if got := strings.Join(fake.calls[1], " "); !strings.Contains(got, "--staged") {
		t.Fatalf("staged args = %q", got)
	}
}

func TestGitDiffCleanReportsNone(t *testing.T) {
	f, m := newFixture(t, "git_diff")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Git: &fakeGitCLI{out: ""}, Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "git_diff", `{}`)
	if isErr || !strings.Contains(out, "no changes") {
		t.Fatalf("clean diff = %q err=%v", out, isErr)
	}
}

func TestGitDiffRefusesWithoutCLI(t *testing.T) {
	f, m := newFixture(t, "git_diff")
	h := Deps{Agent: m, WS: f.ws, Workdir: memberDir(t, f.ws, "api"), Approvals: f.approvals}.Handler()
	out, isErr := call(h, t, "git_diff", `{}`)
	if !isErr || !strings.Contains(out, "git CLI unavailable") {
		t.Fatalf("no-cli diff = %q isErr=%v", out, isErr)
	}
}

func TestGitCommitServed(t *testing.T) {
	if !Serves("git_commit") {
		t.Fatal("git_commit is not a served slug")
	}
}

func testIdentity() gitcore.IdentityFunc {
	return func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
	}
}

func TestGitCommitCreatesCommit(t *testing.T) {
	f, m := newFixture(t, "git_commit")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	writeFile(t, api, "a.go", "package a // v2\n")
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Identity: testIdentity(), Approvals: f.approvals}.Handler()

	resolve := callAsync(h, "git_commit", `{"message":"v2"}`, f)
	if out, isErr := resolve(t); isErr {
		t.Fatalf("commit refused: %s", out)
	}
	repo, err := git.PlainOpen(api)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	c, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if c.Message != "v2" || c.Author.Name != "Ada" || c.Author.Email != "ada@example.com" {
		t.Fatalf("commit = %q by %s <%s>", c.Message, c.Author.Name, c.Author.Email)
	}
}

func TestGitCommitDeniedLeavesNoCommit(t *testing.T) {
	f, m := newFixture(t, "git_commit")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	writeFile(t, api, "a.go", "package a // v2\n")
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Identity: testIdentity(), Approvals: f.approvals}.Handler()

	if out, isErr := callDenied(h, t, "git_commit", `{"message":"nope"}`, f); !isErr {
		t.Fatalf("denied commit must refuse, got %q", out)
	}
	repo, _ := git.PlainOpen(api)
	head, _ := repo.Head()
	c, _ := repo.CommitObject(head.Hash())
	if c.Message != "initial commit" {
		t.Fatalf("denied commit changed HEAD: %q", c.Message)
	}
}

func TestGitCommitRefusesWithoutIdentity(t *testing.T) {
	f, m := newFixture(t, "git_commit")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	writeFile(t, api, "a.go", "package a // v2\n")
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "git_commit", `{"message":"v2"}`, f)
	if out, isErr := resolve(t); !isErr || !strings.Contains(out, "identity") {
		t.Fatalf("no-identity commit = %q isErr=%v", out, isErr)
	}
}

func TestGitCommitRefusesNothingStaged(t *testing.T) {
	f, m := newFixture(t, "git_commit")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Identity: testIdentity(), Approvals: f.approvals}.Handler()
	resolve := callAsync(h, "git_commit", `{"message":"empty"}`, f)
	if out, isErr := resolve(t); !isErr || !strings.Contains(out, "staged") {
		t.Fatalf("clean commit = %q isErr=%v, want nothing-staged refusal", out, isErr)
	}
}

func TestGitCommitBlockedByWorkflow(t *testing.T) {
	f, m := newFixture(t, "git_commit")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	writeFile(t, api, "a.go", "package a // v2\n")
	h := Deps{
		Agent: m, WS: f.ws, Workdir: api, Identity: testIdentity(), Approvals: f.approvals,
		Gate: func(seam string) []string {
			if seam == "git:commit" {
				return []string{"step worktree_create requires a worktree first"}
			}
			return nil
		},
	}.Handler()
	if out, isErr := call(h, t, "git_commit", `{"message":"v2"}`); !isErr || !strings.Contains(out, "workflow blocks commit") {
		t.Fatalf("workflow-blocked commit = %q isErr=%v", out, isErr)
	}
}

func TestGitCommitEnforcesConventions(t *testing.T) {
	f, m := newFixture(t, "git_commit")
	api := memberDir(t, f.ws, "api")
	initRepo(t, api)
	writeFile(t, api, "a.go", "package a // v2\n")
	conv := conventions.Defaults()
	conv.Commit.Format = conventions.FormatConventional
	conv.Commit.CoAuthor = "Bot <bot@example.com>"
	h := Deps{Agent: m, WS: f.ws, Workdir: api, Identity: testIdentity(),
		Approvals: f.approvals, Conventions: &conv}.Handler()

	// A non-conventional subject is refused before an approval is spent.
	if out, isErr := call(h, t, "git_commit", `{"message":"did stuff"}`); !isErr || !strings.Contains(out, "conventional") {
		t.Fatalf("bad subject = %q isErr=%v", out, isErr)
	}
	resolve := callAsync(h, "git_commit", `{"message":"feat(api): add v2"}`, f)
	if out, isErr := resolve(t); isErr {
		t.Fatalf("valid commit refused: %s", out)
	}
	repo, _ := git.PlainOpen(api)
	head, _ := repo.Head()
	c, _ := repo.CommitObject(head.Hash())
	want := "feat(api): add v2\n\nCo-Authored-By: Bot <bot@example.com>\n"
	if c.Message != want {
		t.Fatalf("message = %q, want %q", c.Message, want)
	}
}
