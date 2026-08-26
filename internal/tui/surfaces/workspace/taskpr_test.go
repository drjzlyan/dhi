package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// ghStub implements review.GH for wsview-level tests.
type ghStub struct {
	created []string
}

func (g *ghStub) Available() bool { return true }
func (g *ghStub) AuthToken(context.Context) (string, error) {
	return "", context.Canceled
}
func (g *ghStub) PR(context.Context, string, string) (review.PRMeta, error) {
	return review.PRMeta{}, nil
}
func (g *ghStub) Diff(context.Context, string, string) (string, error) { return "", nil }
func (g *ghStub) PostComment(context.Context, string, string, string) error {
	return nil
}
func (g *ghStub) CreatePR(_ context.Context, _, title, _, base, head string) (review.PRMeta, error) {
	g.created = append(g.created, head+"|"+base+"|"+title)
	return review.PRMeta{Number: 21, URL: "https://github.com/acme/api/pull/21",
		BaseRef: base}, nil
}
func (g *ghStub) ReviewComments(context.Context, string, string) ([]review.RemoteComment, error) {
	return nil, nil
}
func (g *ghStub) IssueComments(context.Context, string, string) ([]review.RemoteComment, error) {
	return nil, nil
}

// gitTaskFixture builds a workspace with a real cloned member "api" (bare
// origin), a task card with a matching changeset, and a review service
// backed by the gh stub.
func gitTaskFixture(t *testing.T) (*Model, *tasks.Store, *review.Store, *ghStub) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ctx := context.Background()

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
	_ = head
	if err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "origin.git")
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}}); err != nil {
		t.Fatal(err)
	}
	sr, err := gitcore.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := sr.Push(ctx, "", "refs/heads/master:refs/heads/master", nil); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema = 1\n\n[members.api]\npath = \"api\"\n"
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	memPath := filepath.Join(root, "api")
	if _, err := gitcore.Clone(ctx, bare, memPath); err != nil {
		t.Fatal(err)
	}
	mr, _ := git.PlainOpen(memPath)
	h, _ := mr.Head()
	if err := mr.Storer.SetReference(plumbing.NewHashReference(
		plumbing.ReferenceName("refs/heads/task/feat-1"), h.Hash())); err != nil {
		t.Fatal(err)
	}

	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.Create("feat-1", "Ship feat", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := ts.RecordChangeSet("feat-1", tasks.ChangeSet{
		Member: "api", Branch: "task/feat-1",
		Path: filepath.Join(".dhi/tasks/feat-1/api"),
	}); err != nil {
		t.Fatal(err)
	}
	rs, err := review.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	stub := &ghStub{}
	svc := review.NewService(ws, rs, nil, stub)

	m := New("test", ws, Deps{Tasks: ts, ReviewSvc: svc})
	m.Resize(110, 34)
	// land on the card in TASKS
	for m.sec != secTasks {
		m.HandleKey("]")
	}
	m.cursors[secTasks] = 0
	return m, ts, rs, stub
}

func TestTaskCreatePRFlow(t *testing.T) {
	// validation: card without worktree refused before any modal
	m0, _, _, _ := gitTaskFixture(t)
	if err := m0.taskStore.Create("nowt", "No worktree", "", ""); err != nil {
		t.Fatal(err)
	}
	m0.cursors[secTasks] = 1 // "nowt" sorts after "feat-1"
	if !m0.HandleKey("p") {
		t.Fatal("p not consumed")
	}
	if m0.form.kind != fNone || m0.form.err == "" ||
		!strings.Contains(m0.form.err, "no worktree") {
		t.Fatalf("refusal wrong: kind=%v err=%q", m0.form.kind, m0.form.err)
	}

	// happy path on feat-1
	m2, ts2, _, stub2 := gitTaskFixture(t)
	m2.cursors[secTasks] = 0
	if !m2.HandleKey("p") {
		t.Fatal("p not consumed")
	}
	if m2.form.kind != fTaskPR {
		t.Fatalf("kind = %v", m2.form.kind)
	}
	if got := m2.form.fields[1].text(); got != "main" {
		t.Errorf("base default = %q", got)
	}
	m2.form.fields[1].runes = []rune("master")
	m2.submitForm()

	msg := pumpEvent(t, m2)
	_ = m2.Update(msg)
	if len(stub2.created) != 1 {
		t.Fatalf("gh not called: %v", stub2.created)
	}
	parts := strings.Split(stub2.created[0], "|")
	if parts[0] != "task/feat-1" || parts[1] != "master" {
		t.Errorf("gh args = %q", stub2.created[0])
	}
	got, _ := ts2.Get("feat-1")
	if got.PRNumber != 21 || got.PRURL == "" {
		t.Fatalf("card PR fields = %d %q", got.PRNumber, got.PRURL)
	}
	if out := ansi.Strip(m2.View()); !strings.Contains(out, "created for feat-1") {
		t.Errorf("flash missing:\n%s", out)
	}
}

func pumpEvent(t *testing.T, m *Model) any {
	t.Helper()
	type boxed struct {
		msg any
	}
	ch := make(chan boxed, 1)
	go func() {
		cmd := m.listen()
		ch <- boxed{cmd()}
	}()
	select {
	case b := <-ch:
		return b.msg
	case <-time.After(3 * time.Second):
		t.Fatal("event never arrived")
	}
	return nil
}
