package reviewer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
)

func timeNow() time.Time { return time.Now() }

// ghFake records the posted comment; implements review.GH.
type ghFake struct {
	body string
	err  error
}

func (g *ghFake) Available() bool                           { return true }
func (g *ghFake) AuthToken(context.Context) (string, error) { return "tok", nil }
func (g *ghFake) PR(context.Context, string, string) (review.PRMeta, error) {
	return review.PRMeta{}, nil
}
func (g *ghFake) Diff(context.Context, string, string) (string, error) { return "", nil }
func (g *ghFake) PostComment(_ context.Context, repo, number, body string) error {
	if g.err != nil {
		return g.err
	}
	g.body = repo + "#" + number + "\n" + body
	return nil
}
func (g *ghFake) CreatePR(context.Context, string, string, string, string, string) (review.PRMeta, error) {
	return review.PRMeta{}, nil
}
func (g *ghFake) ReviewComments(context.Context, string, string) ([]review.RemoteComment, error) {
	return nil, nil
}
func (g *ghFake) IssueComments(context.Context, string, string) ([]review.RemoteComment, error) {
	return nil, nil
}

// prFixture builds an open submitted PR review with cached files and an
// agent-authored mirrored comment.
func prFixture(t *testing.T) (*Model, *review.Store, *ghFake) {
	t.Helper()
	m, ws, st, _ := newSurface(t)
	fgh := &ghFake{}
	svc2 := review.NewService(ws, st, nil, fgh)
	m.svc = svc2

	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = ts

	r := review.Review{
		ID: "api-pr-42", Title: "Add feature",
		Target:    review.Target{Kind: review.KindPR, Member: "api", Base: "main", Head: "abc1234", PRNumber: 42},
		Status:    review.Submitted,
		Viewed:    map[string]bool{},
		Channel:   "#api-pr-42",
		WorkRel:   ".dhi/reviews/api-pr-42/api",
		CreatedAt: timeNow(), UpdatedAt: timeNow(),
	}
	r.Threads = []review.Thread{{ID: 1, File: "main.go", Line: 2, Comments: []review.Comment{
		{Author: "you", Text: "rename this"},
		{Author: "rev", Text: "checked: fine"},
	}}}
	if err := st.Create(r); err != nil {
		t.Fatal(err)
	}
	m.openID = r.ID
	m.files = gitdiff.Parse(samplePatch)
	m.diffFor = r.ID
	return m, st, fgh
}

func TestPostToPRWithAttribution(t *testing.T) {
	m, st, fgh := prFixture(t)
	m.cursors[secReviews] = 0
	m.postToPR()

	msg := pumpCmd(t, m.listen())
	_ = m.Update(msg)
	if m.opErr != "" || m.busy {
		t.Fatalf("err=%q busy=%v", m.opErr, m.busy)
	}
	if !strings.Contains(fgh.body, "github.com/acme/api") || !strings.Contains(fgh.body, "#42\n") {
		t.Fatalf("posted to wrong target: %q", fgh.body)
	}
	if !strings.Contains(fgh.body, "rename this") ||
		!strings.Contains(fgh.body, "- checked: fine — _DHI agent @rev_") {
		t.Fatalf("attribution wrong:\n%s", fgh.body)
	}
	got, _ := st.Get("api-pr-42")
	if !got.Posted {
		t.Error("Posted flag not recorded")
	}

	// branch-backed review refuses posting
	br := review.Review{ID: "api-branch-x", Target: review.Target{
		Kind: review.KindBranch, Member: "api", Base: "main", Head: "dev"},
		Status: review.Submitted, Viewed: map[string]bool{},
		CreatedAt: timeNow(), UpdatedAt: timeNow()}
	if err := st.Create(br); err != nil {
		t.Fatal(err)
	}
	m.openID = br.ID
	m.postToPR()
	if !strings.Contains(m.opErr, "not a PR review") {
		t.Fatalf("opErr = %q", m.opErr)
	}
}

func TestDispatchFixerBindsSameWorktree(t *testing.T) {
	m, st, _ := prFixture(t)
	m.dispatchFixer()
	if m.opErr != "" {
		t.Fatalf("opErr = %q", m.opErr)
	}
	tk, ok := m.taskStore.Get("fix-api-pr-42")
	if !ok {
		t.Fatal("fixer card missing")
	}
	if tk.Status != tasks.Active {
		t.Errorf("status = %s", tk.Status)
	}
	if len(tk.ChangeSets) != 1 || tk.ChangeSets[0].Path != ".dhi/reviews/api-pr-42/api" ||
		tk.ChangeSets[0].Branch != "review/api-pr-42" || tk.ChangeSets[0].Member != "api" {
		t.Fatalf("changesets = %+v", tk.ChangeSets)
	}
	if tk.ThreadChannel != "#api-pr-42" {
		t.Errorf("thread binding = %q", tk.ThreadChannel)
	}

	// duplicate dispatch refused visibly
	m.closeForm()
	m.dispatchFixer()
	if !strings.Contains(m.opErr, "already exists") {
		t.Fatalf("dup opErr = %q", m.opErr)
	}
	_ = st
}

func TestHandoffOpensChangedFiles(t *testing.T) {
	m, _, _ := prFixture(t)
	var got []string
	called := false
	m.openInEditor = func(paths []string) bool {
		called = true
		got = paths
		return true
	}
	m.handoffToEditor()
	if !called || len(got) != 2 {
		t.Fatalf("paths = %v", got)
	}
	for i, want := range []string{"main.go", "util/util.go"} {
		if !strings.HasSuffix(got[i], want) {
			t.Errorf("path[%d] = %q, want suffix %q", i, got[i], want)
		}
	}

	// refusal surfaces
	m.openInEditor = func([]string) bool { return false }
	m.handoffToEditor()
	if !strings.Contains(m.opErr, "refused") {
		t.Fatalf("opErr = %q", m.opErr)
	}
}
