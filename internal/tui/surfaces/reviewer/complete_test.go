package reviewer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func timeNow() time.Time { return time.Now() }

// ghFake records the posted comment; implements review.GH.
type ghFake struct {
	body           string
	err            error
	created        []string
	reviewComments []review.RemoteComment
	issueComments  []review.RemoteComment
	// posted records review-comment posts as "file:line:side:replyTo|body".
	posted []string
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
func (g *ghFake) CreatePR(_ context.Context, _, title, _, base, head string) (review.PRMeta, error) {
	g.created = append(g.created, head+"|"+base)
	return review.PRMeta{Number: 7, Title: title,
		URL: "https://github.com/acme/api/pull/7", BaseRef: base}, nil
}
func (g *ghFake) PostReviewComment(_ context.Context, _, _, _, path string, line int, side, body string, inReplyTo int64) error {
	if g.err != nil {
		return g.err
	}
	g.posted = append(g.posted, fmt.Sprintf("%s:%d:%s:%d|%s", path, line, side, inReplyTo, body))
	return nil
}
func (g *ghFake) ReviewComments(context.Context, string, string) ([]review.RemoteComment, error) {
	return g.reviewComments, nil
}
func (g *ghFake) IssueComments(context.Context, string, string) ([]review.RemoteComment, error) {
	return g.issueComments, nil
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

func TestPostToPRExternalPublishesConsolidated(t *testing.T) {
	m, st, fgh := prFixture(t)
	m.cursors[secReviews] = 0
	// Add a resolved thread and a pending draft: neither may be posted.
	resID, err := st.AddThread("api-pr-42", review.Thread{File: "gone.go", Line: 1,
		Comments: []review.Comment{{Author: "you", Text: "old nit"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetResolved("api-pr-42", resID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddThread("api-pr-42", review.Thread{File: "draft.go", Line: 5,
		Comments: []review.Comment{{Author: "you", Text: "draft", Pending: true}}}); err != nil {
		t.Fatal(err)
	}

	m.postToPR()

	msg := pumpCmd(t, m.listen())
	_ = m.Update(msg)
	if m.opErr != "" || m.busy {
		t.Fatalf("err=%q busy=%v", m.opErr, m.busy)
	}
	if !strings.Contains(fgh.body, "#42") {
		t.Fatalf("posted to wrong target: %q", fgh.body)
	}
	if !strings.Contains(fgh.body, "rename this") ||
		!strings.Contains(fgh.body, "checked: fine") {
		t.Fatalf("summary missing comments:\n%s", fgh.body)
	}
	if strings.Contains(fgh.body, "_DHI agent") {
		t.Fatalf("external PR must not expose agent attribution:\n%s", fgh.body)
	}
	if strings.Contains(fgh.body, "old nit") || strings.Contains(fgh.body, "draft") {
		t.Fatalf("resolved/pending threads must not be posted:\n%s", fgh.body)
	}
	if len(fgh.posted) != 0 {
		t.Fatalf("external PR must not post threaded review comments: %v", fgh.posted)
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

func TestPostToPROwnPublishesThreaded(t *testing.T) {
	m, ws, st, _ := newSurface(t)
	fgh := &ghFake{}
	m.svc = review.NewService(ws, st, nil, fgh)

	// Own the PR by binding the head branch that exists locally in the
	// fixture member repo.
	mem, ok := ws.Member("api")
	if !ok {
		t.Fatal("member api gone")
	}
	repo, err := gitcore.Open(mem.Path)
	if err != nil {
		t.Fatal(err)
	}
	branches, err := repo.Branches()
	if err != nil || len(branches) == 0 {
		t.Fatalf("fixture member has no branches: %v", err)
	}

	r := review.Review{
		ID: "api-pr-42", Title: "Add feature",
		Target:    review.Target{Kind: review.KindPR, Member: "api", Base: branches[0], Head: "abc1234", PRNumber: 42, HeadBranch: branches[0]},
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

	m.postToPR()

	msg := pumpCmd(t, m.listen())
	_ = m.Update(msg)
	if m.opErr != "" || m.busy {
		t.Fatalf("err=%q busy=%v", m.opErr, m.busy)
	}
	if len(fgh.posted) != 2 {
		t.Fatalf("expected 2 threaded comments, got %v", fgh.posted)
	}
	if !strings.HasPrefix(fgh.posted[0], "main.go:2:RIGHT:0|") ||
		!strings.Contains(fgh.posted[0], "rename this") {
		t.Fatalf("root comment wrong: %q", fgh.posted[0])
	}
	if !strings.Contains(fgh.posted[1], "checked: fine") {
		t.Fatalf("reply comment wrong: %q", fgh.posted[1])
	}
	if fgh.body != "" {
		t.Fatalf("own PR must not post a consolidated comment: %q", fgh.body)
	}
	got, _ := st.Get("api-pr-42")
	if !got.Posted {
		t.Error("Posted flag not recorded")
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

func TestCreatePRFromReviewFlow(t *testing.T) {
	m, ws, st, _ := newSurface(t)
	m.svc = review.NewService(ws, st, nil, &ghFake{})
	r := startBranchReview(t, m, st)
	if seedBranch(t, ws, "review/"+r.ID) == "" {
		t.Fatal("seed failed")
	}

	// branch-backed card starts without a PR
	if r.Target.PRNumber != 0 {
		t.Fatalf("pre-existing PR on fresh card")
	}

	m.HandleKey("esc") // → REVIEWS
	if !m.HandleKey("C") {
		t.Fatal("C not consumed")
	}
	if m.form.kind != fCreatePR {
		t.Fatalf("kind = %v", m.form.kind)
	}
	if m.form.fields[0].text() != "master...master" {
		t.Errorf("title prefill = %q", m.form.fields[0].text())
	}

	// empty base refused
	savedBase := string(m.form.fields[1].runes)
	m.form.fields[1].runes = nil
	m.submitForm()
	if m.form.err == "" {
		t.Fatal("empty base accepted")
	}
	m.form.fields[1].runes = []rune(savedBase)

	// happy path
	m.form.fields[0].runes = []rune("Ship it")
	m.submitForm()
	msg := pumpCmd(t, m.listen())
	_ = m.Update(msg)
	if m.opErr != "" || m.busy {
		t.Fatalf("err=%q busy=%v", m.opErr, m.busy)
	}
	got, ok := st.Get(r.ID)
	if !ok || got.Target.Kind != review.KindPR ||
		got.Target.PRNumber == 0 || got.PRURL == "" {
		t.Fatalf("card after create: %+v url=%q", got.Target, got.PRURL)
	}
	if !strings.Contains(m.form.flash, "PR #") {
		t.Errorf("flash = %q", m.form.flash)
	}

	// pressing C again refuses visibly
	if !m.HandleKey("C") {
		t.Fatal("second C dropped")
	}
	if !strings.Contains(m.opErr, "already backs PR #") {
		t.Errorf("opErr = %q", m.opErr)
	}
}

// seedBranch creates a local branch ref at the api member's HEAD,
// mirroring what a real review/task worktree checkout produces.
func seedBranch(t *testing.T, ws *workspace.Workspace, branch string) string {
	t.Helper()
	mem, ok := ws.Member("api")
	if !ok {
		t.Fatal("no api member")
	}
	r, err := git.PlainOpen(mem.Path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Storer.SetReference(plumbing.NewHashReference(
		plumbing.ReferenceName("refs/heads/"+branch), h.Hash())); err != nil {
		t.Fatal(err)
	}
	return h.Hash().String()
}

func TestRemoteSyncFlow(t *testing.T) {
	m, ws, st, _ := newSurface(t)
	fgh := &ghFake{}
	fgh.reviewComments = []review.RemoteComment{
		{ID: 30, RootID: 30, Path: "main.go", Line: 1, Side: "RIGHT",
			Body: "upstream note", Author: "amy"},
	}
	m.svc = review.NewService(ws, st, nil, fgh)

	r := review.Review{
		ID: "api-pr-42", Title: "Add feature",
		Target:    review.Target{Kind: review.KindPR, Member: "api", Base: "main", Head: "abc1234", PRNumber: 42},
		Status:    review.Pending,
		Viewed:    map[string]bool{},
		Channel:   "#api-pr-42",
		WorkRel:   ".dhi/reviews/api-pr-42/api",
		CreatedAt: timeNow(), UpdatedAt: timeNow(),
	}
	if err := st.Create(r); err != nil {
		t.Fatal(err)
	}

	// opening a PR-backed review auto-imports in the background
	m.open(r.ID)
	msg := pumpCmd(t, m.listen())
	ev, ok := msg.(revEvent)
	if !ok || ev.kind != evImported || ev.err != "" || ev.n != 1 {
		t.Fatalf("event = %+v", msg)
	}
	_ = m.Update(msg)

	got, _ := st.Get(r.ID)
	if len(got.Threads) != 1 || got.Threads[0].Comments[0].Text != "upstream note" ||
		got.Threads[0].Comments[0].RemoteID != 30 {
		t.Fatalf("threads = %+v", got.Threads)
	}
	if m.syncedAt.IsZero() {
		t.Error("sync timestamp not set")
	}

	// FILES header shows the remote badge
	m.HandleKey("]")
	out := ansiStrip(m.View())
	if !strings.Contains(out, "1 remote") || !strings.Contains(out, "synced ") {
		t.Errorf("badge missing:\n%s", out)
	}

	// R re-imports (no-op now) without error
	if !m.HandleKey("R") {
		t.Fatal("R not consumed")
	}
	msg2 := pumpCmd(t, m.listen())
	_ = m.Update(msg2)
	if m.opErr != "" {
		t.Fatalf("opErr = %q", m.opErr)
	}
}
