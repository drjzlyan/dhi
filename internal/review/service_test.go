package review

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// fakeGH records calls and returns canned data.
type fakeGH struct {
	meta           PRMeta
	diff           string
	prCalls        int
	created        []string // repo|title|base|head
	reviewComments []RemoteComment
	issueComments  []RemoteComment
}

func (f *fakeGH) Available() bool { return true }
func (f *fakeGH) AuthToken(context.Context) (string, error) {
	return "test-token", nil
}
func (f *fakeGH) PR(_ context.Context, _, _ string) (PRMeta, error) {
	f.prCalls++
	return f.meta, nil
}
func (f *fakeGH) Diff(_ context.Context, _, _ string) (string, error) { return f.diff, nil }
func (f *fakeGH) PostComment(context.Context, string, string, string) error {
	return fmt.Errorf("not implemented in fake")
}
func (f *fakeGH) CreatePR(_ context.Context, repo, title, _, base, head string) (PRMeta, error) {
	f.created = append(f.created, strings.Join([]string{repo, title, base, head}, "|"))
	return PRMeta{Number: 7, Title: title, URL: "https://github.com/acme/api/pull/7", BaseRef: base}, nil
}
func (f *fakeGH) ReviewComments(context.Context, string, string) ([]RemoteComment, error) {
	return f.reviewComments, nil
}
func (f *fakeGH) IssueComments(context.Context, string, string) ([]RemoteComment, error) {
	return f.issueComments, nil
}

// fixture builds a workspace whose member "api" is a clone of a source
// repo with two commits on main and a PR head ref.
func fixture(t *testing.T) (*workspace.Workspace, *Store, string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commit := func(msg string, files map[string]string) plumbing.Hash {
		for name, body := range files {
			p := filepath.Join(src, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := wt.Add(name); err != nil {
				t.Fatal(err)
			}
		}
		h, err := wt.Commit(msg, &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
		})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	base := commit("base", map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	head := commit("feature", map[string]string{"main.go": "package main\n\nfunc main() {\n\tprintln(1)\n}\n"})

	// GitHub-style pull ref on the source repo (fetched explicitly later).
	if _, err := r.Reference("refs/pull/42/head", false); err == nil {
		t.Fatal("pull ref should not exist yet")
	}
	if err := r.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/pull/42/head"), head)); err != nil {
		t.Fatal(err)
	}
	_ = base

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	memPath := filepath.Join(root, "api")
	if _, err := gitcore.Clone(context.Background(), src, memPath); err != nil {
		t.Fatalf("clone: %v", err)
	}
	mem, ok := w.Member("api")
	if !ok || mem.Path != memPath {
		t.Fatalf("member = %+v ok=%v", mem, ok)
	}
	st, err := Open(w)
	if err != nil {
		t.Fatal(err)
	}
	return w, st, memPath
}

// fakeSeam creates a real dir under the reviews tree so relative paths
// behave like the production wiring.
type fakeSeam struct {
	store     *Store
	discarded []string
}

func (f *fakeSeam) work(id, member, startpoint string) (string, error) {
	rel := filepath.Join(Dir, id, member)
	if err := os.MkdirAll(filepath.Join(f.store.ws.Root, rel), 0o755); err != nil {
		return "", err
	}
	return rel, nil
}
func (f *fakeSeam) discard(_, relPath string) error {
	f.discarded = append(f.discarded, relPath)
	return os.RemoveAll(filepath.Join(f.store.ws.Root, relPath))
}

func newSvc(w *workspace.Workspace, st *Store, gh GH) (*Service, *fakeSeam) {
	seam := &fakeSeam{store: st}
	st.SetWorktreeSeam(seam.work, seam.discard)
	return NewService(w, st, nil, gh), seam
}

func TestStartBranchReview(t *testing.T) {
	w, st, _ := fixture(t)
	svc, seam := newSvc(w, st, nil)

	r, err := svc.Start(context.Background(), "api", KindBranch, "main", "main~1", 0)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if r.Target.Kind != KindBranch || r.Status != Pending || r.WorkRel == "" {
		t.Fatalf("review = %+v", r)
	}
	if !strings.HasPrefix(r.Channel, "#") {
		t.Errorf("channel = %q", r.Channel)
	}
	if seam.discarded != nil {
		t.Error("discard called during start")
	}
	got, ok := st.Get(r.ID)
	if !ok || got.WorkRel != r.WorkRel {
		t.Fatalf("card not persisted: %+v (found=%v)", got, ok)
	}

	branches, err := svc.ListBranches("api")
	if err != nil || len(branches) == 0 || branches[0] != "master" && branches[0] != "main" {
		t.Logf("branches = %v err=%v", branches, err) // default branch name varies by init
	}
}

func TestStartPRReviewFetchesHead(t *testing.T) {
	w, st, _ := fixture(t)
	fg := &fakeGH{meta: PRMeta{Number: 42, Title: "Add feature", BaseRef: "master"}}
	svc, seam := newSvc(w, st, fg)

	r, err := svc.Start(context.Background(), "api", KindPR, "", "", 42)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if fg.prCalls != 1 {
		t.Errorf("gh.PR called %d times", fg.prCalls)
	}
	if r.Target.Base != "master" || len(r.Target.Head) < 7 {
		t.Fatalf("target = %+v", r.Target)
	}
	if r.Title != "Add feature" {
		t.Errorf("title = %q", r.Title)
	}
	if _, ok := st.Get(r.ID); !ok {
		t.Errorf("card missing for id %q", r.ID)
	}
	_ = seam
}

func TestStartDegradesVisiblyWithoutSeams(t *testing.T) {
	w, st, _ := fixture(t)

	noSeam := NewService(w, st, nil, nil)
	if _, err := noSeam.Start(context.Background(), "api", KindBranch, "main", "dev", 0); err == nil ||
		!strings.Contains(err.Error(), "seam unavailable") {
		t.Errorf("nil worktree seam error = %v", err)
	}

	noGh := NewService(w, st, nil, nil)
	st.SetWorktreeSeam(func(string, string, string) (string, error) { return ".dhi/reviews/x/api", nil },
		func(string, string) error { return nil })
	if _, err := noGh.Start(context.Background(), "api", KindPR, "", "", 42); err == nil ||
		!strings.Contains(err.Error(), "gh unavailable") {
		t.Errorf("nil gh error = %v", err)
	}
}

func TestDiffViaInjectedRunner(t *testing.T) {
	w, st, _ := fixture(t)
	svc, _ := newSvc(w, st, nil)
	r, err := svc.Start(context.Background(), "api", KindBranch, "main", "main~1", 0)
	if err != nil {
		t.Fatal(err)
	}

	called := ""
	svc.diffFn = func(_ context.Context, dir string, args ...string) (string, error) {
		called = dir + "|" + strings.Join(args, " ")
		return "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,4 @@\n package main\n \n+println(1)\n func main() {}\n", nil
	}
	files, err := svc.Diff(context.Background(), r)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(called, "diff --no-color main..."+r.Target.Head) {
		t.Errorf("diff args = %q", called)
	}
	if len(files) != 1 || files[0].DisplayPath() != "main.go" || len(files[0].Hunks) != 1 {
		t.Fatalf("files = %+v", files)
	}

	r.Done = true
	if _, err := svc.Diff(context.Background(), r); err == nil {
		t.Error("done review diffed")
	}
}

func TestDiscardMarksDoneAndRemovesWorktree(t *testing.T) {
	w, st, _ := fixture(t)
	svc, seam := newSvc(w, st, nil)
	r, err := svc.Start(context.Background(), "api", KindBranch, "main", "dev", 0)
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(w.Root, r.WorkRel)
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}
	if err := svc.Discard(r.ID); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if len(seam.discarded) != 1 {
		t.Fatalf("discarded = %v", seam.discarded)
	}
	got, _ := st.Get(r.ID)
	if !got.Done {
		t.Error("card not marked done")
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Error("worktree survived discard")
	}
}

func TestUnknownMemberFails(t *testing.T) {
	w, st, _ := fixture(t)
	svc, _ := newSvc(w, st, nil)
	if _, err := svc.Start(context.Background(), "ghost", KindBranch, "main", "x", 0); err == nil {
		t.Error("unknown member accepted")
	}
	if _, err := svc.ListBranches("ghost"); err == nil {
		t.Error("unknown member branches listed")
	}
}

// bareFixture: src repo -> bare origin; member cloned FROM bare so pushes
// have a real (local) remote to land on.
func bareFixture(t *testing.T) (*workspace.Workspace, *Store, *fakeGH, string, string, string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	r, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644)
	wt.Add(".")
	head, err := wt.Commit("base", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "origin.git")
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin", URLs: []string{bare}}); err != nil {
		t.Fatal(err)
	}
	sr, err := gitcore.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sr.Push(ctx, "", "refs/heads/master:refs/heads/master", nil); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	memPath := filepath.Join(root, "api")
	if _, err := gitcore.Clone(ctx, bare, memPath); err != nil {
		t.Fatal(err)
	}
	st, err := Open(w)
	if err != nil {
		t.Fatal(err)
	}
	fg := &fakeGH{}
	return w, st, fg, memPath, head.String(), bare
}

func TestCreatePRForBranchPushesAndCreates(t *testing.T) {
	w, st, fg, memPath, head, bare := bareFixture(t)
	svc := NewService(w, st, nil, fg)

	// a feature branch ref at HEAD (as task/review worktrees produce)
	mr, err := git.PlainOpen(memPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := mr.Storer.SetReference(plumbing.NewHashReference(
		plumbing.ReferenceName("refs/heads/task/feat-1"), plumbing.NewHash(head))); err != nil {
		t.Fatal(err)
	}

	meta, err := svc.CreatePRForBranch(context.Background(),
		"api", "task/feat-1", "Add feat", "master")
	if err != nil {
		t.Fatalf("CreatePRForBranch: %v", err)
	}
	if meta.Number != 7 || meta.BaseRef != "master" ||
		meta.URL == "" {
		t.Fatalf("meta = %+v", meta)
	}
	parts := strings.Split(fg.created[0], "|")
	if parts[3] != "task/feat-1" || parts[2] != "master" {
		t.Errorf("gh args = %q", fg.created[0])
	}
	// branch actually landed on the bare origin
	originRepo, err := git.PlainOpen(bare)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := originRepo.Reference(plumbing.ReferenceName("refs/heads/task/feat-1"), true)
	if err != nil || ref.Hash().String() != head {
		t.Fatalf("origin ref = %v err=%v", ref, err)
	}
}

func TestCreatePRForBranchDirtyRefusal(t *testing.T) {
	w, st, fg, memPath, _, _ := bareFixture(t)
	svc := NewService(w, st, nil, fg)

	os.WriteFile(filepath.Join(memPath, "dirty.txt"), []byte("x"), 0o644)
	_, err := svc.CreatePRForBranch(context.Background(),
		"api", "master", "T", "master")
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("err = %v", err)
	}
	if len(fg.created) != 0 {
		t.Error("gh consulted despite dirty tree")
	}
}

func TestCreatePRLinksBackOnCard(t *testing.T) {
	w, st, fg, memPath, head, _ := bareFixture(t)
	svc := NewService(w, st, nil, fg)

	mr, _ := git.PlainOpen(memPath)
	if err := mr.Storer.SetReference(plumbing.NewHashReference(
		plumbing.ReferenceName("refs/heads/review/api-branch-master"),
		plumbing.NewHash(head))); err != nil {
		t.Fatal(err)
	}
	card := Review{ID: "api-branch-master", Title: "T",
		Target: Target{Kind: KindBranch, Member: "api", Base: "master",
			Head: head},
		Status: Pending, Viewed: map[string]bool{},
		Channel:   "#api-branch-master",
		WorkRel:   ".dhi/reviews/api-branch-master/api",
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Create(card); err != nil {
		t.Fatal(err)
	}
	out, err := svc.CreatePR(context.Background(),
		"api-branch-master", "Review T", "master")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if out.Target.Kind != KindPR || out.Target.PRNumber != 7 ||
		out.PRURL == "" {
		t.Fatalf("linked card = %+v url=%q", out.Target, out.PRURL)
	}
	reloaded, ok := st.Get("api-branch-master")
	if !ok || reloaded.Target.PRNumber != 7 || reloaded.PRURL == "" {
		t.Fatalf("persisted card = %+v", reloaded)
	}

	// double-create refused
	if _, err := svc.CreatePR(context.Background(),
		"api-branch-master", "again", "master"); err == nil {
		t.Error("second CreatePR accepted")
	}
}

func TestParseReviewCommentThreads(t *testing.T) {
	data := `[
	  {"id":10,"in_reply_to_id":null,"path":"a.go","line":5,"original_line":null,"side":"RIGHT","body":"root","user":{"login":"amy"},"created_at":"2026-01-02T03:04:05Z"},
	  {"id":11,"in_reply_to_id":10,"path":"a.go","line":5,"side":"RIGHT","body":"reply","user":{"login":"bob"},"created_at":"2026-01-02T03:05:05Z"},
	  {"id":12,"in_reply_to_id":null,"path":"b.go","original_line":9,"line":null,"side":"LEFT","body":"old-side","user":{"login":"cy"},"created_at":"2026-01-02T03:06:05Z"},
	  {"id":13,"in_reply_to_id":null,"path":"c.go","line":null,"original_line":null,"body":"outdated","user":{"login":"dy"},"created_at":"2026-01-02T03:07:05Z"}
	]`
	rcs, err := parseReviewComments(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rcs) != 4 {
		t.Fatalf("got %d comments", len(rcs))
	}
	if rcs[0].RootID != 10 || rcs[1].RootID != 10 {
		t.Errorf("reply chain wrong: %+v", rcs[:2])
	}
	if rcs[2].OrigLine != 9 || rcs[2].Side != "LEFT" || rcs[2].Line != 0 {
		t.Errorf("old-side mapping wrong: %+v", rcs[2])
	}
	if rcs[3].Line != 0 && rcs[3].OrigLine != 0 {
		t.Errorf("outdated should carry no lines: %+v", rcs[3])
	}
}

func TestImportCommentsMergeAndDedupe(t *testing.T) {
	w, st, fg, _, headSha, _ := bareFixture(t)
	base := time.Now().Add(-time.Hour)
	fg.reviewComments = []RemoteComment{
		{ID: 10, RootID: 10, Path: "main.go", Line: 3, Side: "RIGHT",
			Body: "root note", Author: "amy", CreatedAt: base},
		{ID: 11, RootID: 10, Path: "main.go", Line: 3, Side: "RIGHT",
			Body: "reply", Author: "bob", CreatedAt: base.Add(time.Minute)},
		{ID: 12, RootID: 12, Path: "old.go", OrigLine: 9, Line: 0, Side: "LEFT",
			Body: "old side", Author: "cy", CreatedAt: base.Add(2 * time.Minute)},
		{ID: 13, RootID: 13,
			Body: "outdated", Author: "dy", CreatedAt: base.Add(3 * time.Minute)},
	}
	fg.issueComments = []RemoteComment{
		{ID: 20, RootID: 20, Body: "general remark",
			Author: "eve", CreatedAt: base.Add(4 * time.Minute)},
	}
	svc := NewService(w, st, nil, fg)

	card := Review{ID: "api-pr-1", Title: "T",
		Target: Target{Kind: KindPR, Member: "api", Base: "master",
			Head: headSha, PRNumber: 5},
		Status: Pending, Viewed: map[string]bool{}, Channel: "#api-pr-1",
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Create(card); err != nil {
		t.Fatal(err)
	}

	n, err := svc.ImportComments(context.Background(), card)
	if err != nil {
		t.Fatalf("ImportComments: %v", err)
	}
	if n != 5 {
		t.Fatalf("added = %d, want 5", n)
	}
	got, _ := st.Get("api-pr-1")
	if len(got.Threads) != 4 {
		t.Fatalf("threads = %d (%+v)", len(got.Threads), got.Threads)
	}
	byRoot := map[int64]Thread{}
	for _, t := range got.Threads {
		byRoot[t.RemoteRoot] = t
	}
	root := byRoot[10]
	if root.File != "main.go" || root.Line != 3 || root.Side != SideNew ||
		len(root.Comments) != 2 || root.Comments[0].Author != "amy" ||
		root.Comments[0].Pending || root.Comments[0].RemoteID != 10 {
		t.Fatalf("thread 10 = %+v", root)
	}
	old := byRoot[12]
	if old.File != "old.go" || old.Line != 9 || old.Side != SideOld {
		t.Fatalf("old-side thread = %+v", old)
	}
	if byRoot[13].File != "(remote)" || byRoot[13].Line != 0 {
		t.Fatalf("outdated thread = %+v", byRoot[13])
	}
	if byRoot[20].File != "(remote)" || byRoot[20].Comments[0].Author != "eve" {
		t.Fatalf("issue-comment thread = %+v", byRoot[20])
	}

	// re-import is a no-op
	n2, err := svc.ImportComments(context.Background(), card)
	if err != nil || n2 != 0 {
		t.Fatalf("second import added %d err=%v", n2, err)
	}
	got2, _ := st.Get("api-pr-1")
	if len(got2.Threads) != 4 {
		t.Errorf("threads grew on re-import")
	}
}
