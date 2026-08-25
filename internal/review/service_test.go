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
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// fakeGH records calls and returns canned data.
type fakeGH struct {
	meta    PRMeta
	diff    string
	prCalls int
}

func (f *fakeGH) Available() bool { return true }
func (f *fakeGH) PR(_ context.Context, _, _ string) (PRMeta, error) {
	f.prCalls++
	return f.meta, nil
}
func (f *fakeGH) Diff(_ context.Context, _, _ string) (string, error) { return f.diff, nil }
func (f *fakeGH) PostComment(context.Context, string, string, string) error {
	return fmt.Errorf("not implemented in fake")
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
