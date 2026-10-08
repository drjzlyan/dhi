package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const subPatch = `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
 package main
 
-func main() {}
+func main() {
+	println(1)
+}
`

// submitFixture builds a PR-backed review whose diff is subPatch (new lines
// 1-5, old lines 1-3) with a fake gh.
func submitFixture(t *testing.T) (*Service, *Store, *fakeGH, Review) {
	t.Helper()
	w, st, _ := fixture(t)
	gh := &fakeGH{login: "me"}
	gh.meta = PRMeta{Number: 7, Author: "someone-else", HeadSHA: "deadbeef"}
	svc := NewService(w, st, nil, gh)
	svc.SetDiffForTest(func(context.Context, string, ...string) (string, error) { return subPatch, nil })
	root := filepath.Join(w.Root, ".dhi", "reviews", "wt")
	os.MkdirAll(root, 0o755)
	r := Review{ID: "pr-7", Title: "t", Status: Pending, Viewed: map[string]bool{},
		Target:  Target{Kind: KindPR, Member: "api", Base: "main", Head: "deadbeef", PRNumber: 7, HeadBranch: "feat"},
		WorkRel: ".dhi/reviews/wt", Channel: "#review-pr-7"}
	if err := st.Create(r); err != nil {
		t.Fatal(err)
	}
	return svc, st, gh, r
}

func addHuman(t *testing.T, st *Store, id, file string, line int, side Side, text string) int64 {
	t.Helper()
	tid, err := st.AddThread(id, Thread{File: file, Line: line, Side: side,
		Comments: []Comment{{Author: Human, Text: text, Pending: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return tid
}

func TestOwnerRepoNormalisesEveryRemoteForm(t *testing.T) {
	cases := map[string][2]string{
		"https://github.com/acme/api.git":      {"", "acme/api"},
		"https://github.com/acme/api":          {"", "acme/api"},
		"https://github.com/acme/api/":         {"", "acme/api"},
		"git@github.com:acme/api.git":          {"", "acme/api"},
		"ssh://git@github.com/acme/api.git":    {"", "acme/api"},
		"ssh://git@github.com:22/acme/api.git": {"", "acme/api"},
		"github.com/acme/api":                  {"", "acme/api"},
		"acme/api":                             {"", "acme/api"},
		"https://ghe.corp.io/acme/api.git":     {"ghe.corp.io", "acme/api"},
		"git@ghe.corp.io:acme/api.git":         {"ghe.corp.io", "acme/api"},
	}
	for in, want := range cases {
		host, repo := ownerRepo(in)
		if host != want[0] || repo != want[1] {
			t.Errorf("ownerRepo(%q) = %q, %q; want %q, %q", in, host, repo, want[0], want[1])
		}
	}
	got := strings.Join(apiArgs("git@ghe.corp.io:acme/api.git", "pulls/7/reviews", "--method", "POST"), " ")
	if got != "api --hostname ghe.corp.io --method POST repos/acme/api/pulls/7/reviews" {
		t.Errorf("apiArgs = %q", got)
	}
}

func TestSubmitSendsOneReviewWithInlineCommentsAsTheHuman(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	addHuman(t, st, r.ID, "main.go", 4, SideNew, "why println?")
	addHuman(t, st, r.ID, "main.go", 3, SideOld, "this was fine")
	addHuman(t, st, r.ID, "main.go", 0, SideNew, "file-level thought")

	plan, url, err := svc.SubmitReview(context.Background(), r, VerdictRequestChanges, "Please rework.")
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.submitted) != 1 {
		t.Fatalf("%d reviews sent, want exactly one", len(gh.submitted))
	}
	sub := gh.submitted[0]
	if sub.Event != EventRequestChanges || sub.Body != "Please rework." || sub.CommitSHA != "deadbeef" {
		t.Fatalf("submission = %+v", sub)
	}
	if len(sub.Comments) != 3 {
		t.Fatalf("comments = %+v", sub.Comments)
	}
	want := map[string]ReviewCommentInput{
		"why println?":       {Path: "main.go", Line: 4, Side: "RIGHT", Body: "why println?"},
		"this was fine":      {Path: "main.go", Line: 3, Side: "LEFT", Body: "this was fine"},
		"file-level thought": {Path: "main.go", SubjectType: "file", Body: "file-level thought"},
	}
	for _, c := range sub.Comments {
		if w := want[c.Body]; w != c {
			t.Errorf("comment %q = %+v, want %+v", c.Body, c, w)
		}
	}
	if !strings.Contains(url, "pullrequestreview-9") || plan.InlineCount() != 3 {
		t.Fatalf("url=%q plan=%+v", url, plan)
	}
	if gh.submitRepo == "" {
		t.Fatal("repo not passed")
	}
	cur, _ := st.Get(r.ID)
	if !cur.Posted || cur.Verdict != "request_changes" || cur.Summary != "Please rework." || cur.PRURL != url || cur.Status != Submitted {
		t.Fatalf("review not recorded: %+v", cur)
	}
	for _, th := range cur.Threads {
		if !th.Comments[0].Posted || th.Comments[0].Pending {
			t.Errorf("thread %d comment not marked posted", th.ID)
		}
	}
}

func TestAgentSuggestionsAndNamesNeverGoOut(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	addHuman(t, st, r.ID, "main.go", 4, SideNew, "mine")
	// An open suggestion, a dismissed one, and an employee reply in a human thread.
	st.AddThread(r.ID, Thread{File: "main.go", Line: 4, Side: SideNew, Comments: []Comment{
		{Author: "sage", Text: "SECRET-SUGGESTION", Suggested: true}}})
	st.AddThread(r.ID, Thread{File: "main.go", Line: 5, Side: SideNew, Comments: []Comment{
		{Author: "sage", Text: "SECRET-DISMISSED", Suggested: true, Dismissed: true}}})
	cur, _ := st.Get(r.ID)
	st.AppendComment(r.ID, cur.Threads[0].ID, Comment{Author: "atlas", Text: "SECRET-REPLY"})

	plan, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Suggested != 1 {
		t.Fatalf("undecided suggestions = %d, want 1 (reported, not sent)", plan.Suggested)
	}
	blob := gh.submitted[0].Body
	for _, c := range gh.submitted[0].Comments {
		blob += "\n" + c.Body
	}
	for _, leak := range []string{"SECRET", "sage", "atlas", "DHI agent"} {
		if strings.Contains(blob, leak) {
			t.Fatalf("%q leaked into the review:\n%s", leak, blob)
		}
	}
	if len(gh.submitted[0].Comments) != 1 || gh.submitted[0].Comments[0].Body != "mine" {
		t.Fatalf("comments = %+v", gh.submitted[0].Comments)
	}
}

func TestOffDiffCommentsMoveIntoTheBodyInsteadOfFailingTheReview(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	addHuman(t, st, r.ID, "main.go", 4, SideNew, "on the diff")
	addHuman(t, st, r.ID, "main.go", 99, SideNew, "far away")       // line not in the diff
	addHuman(t, st, r.ID, "other.go", 2, SideNew, "file not in it") // file not in the diff

	plan, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, "Summary.")
	if err != nil {
		t.Fatal(err)
	}
	sub := gh.submitted[0]
	if len(sub.Comments) != 1 || sub.Comments[0].Body != "on the diff" {
		t.Fatalf("inline = %+v", sub.Comments)
	}
	for _, want := range []string{"Summary.", "Comments on lines outside the diff", "main.go:99", "far away", "other.go:2", "file not in it"} {
		if !strings.Contains(sub.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, sub.Body)
		}
	}
	if plan.InBody != 2 {
		t.Fatalf("InBody = %d", plan.InBody)
	}
	// Every one of them counts as sent: a second submit has nothing new.
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, ""); err == nil ||
		!strings.Contains(err.Error(), "nothing to send") {
		t.Fatalf("second submit = %v", err)
	}
}

func TestFollowUpsInOneThreadShareOneInlineComment(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	tid := addHuman(t, st, r.ID, "main.go", 4, SideNew, "first")
	st.AppendComment(r.ID, tid, Comment{Author: Human, Text: "second", Pending: true})
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	cs := gh.submitted[0].Comments
	if len(cs) != 1 || cs[0].Body != "first\n\nsecond" {
		t.Fatalf("comments = %+v", cs)
	}
}

func TestRepliesToExistingThreadsThreadProperly(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	tid, _ := st.AddThread(r.ID, Thread{File: "main.go", Line: 4, Side: SideNew, RemoteRoot: 555,
		Comments: []Comment{{Author: "octocat", Text: "remote", RemoteID: 555},
			{Author: Human, Text: "my reply", Pending: true}}})
	_ = tid
	addHuman(t, st, r.ID, "main.go", 5, SideNew, "new thread")
	plan, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.submitted[0].Comments) != 1 || gh.submitted[0].Comments[0].Body != "new thread" {
		t.Fatalf("inline = %+v", gh.submitted[0].Comments)
	}
	if len(gh.replies) != 1 || !strings.Contains(gh.replies[0], "reply-to=555") || !strings.Contains(gh.replies[0], "my reply") {
		t.Fatalf("replies = %v", gh.replies)
	}
	if plan.ReplyCount() != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	cur, _ := st.Get(r.ID)
	if !cur.Threads[0].Comments[1].Posted {
		t.Fatal("the reply was not marked posted")
	}
}

func TestApprovingOrBlockingYourOwnPRIsRefusedWithTheFix(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	gh.meta.Author = "ME" // case-insensitive match with login "me"
	addHuman(t, st, r.ID, "main.go", 4, SideNew, "x")
	for _, v := range []Verdict{VerdictApprove, VerdictRequestChanges} {
		_, _, err := svc.SubmitReview(context.Background(), r, v, "")
		if err == nil || !strings.Contains(err.Error(), "your own pull request") || !strings.Contains(err.Error(), "comment") {
			t.Fatalf("%s: err = %v", v, err)
		}
	}
	if len(gh.submitted) != 0 {
		t.Fatal("a refused review was still sent")
	}
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, ""); err != nil {
		t.Fatalf("a plain comment on your own PR must work: %v", err)
	}
}

func TestApproveNeedsNoBodyAndAnEmptyCommentIsRefused(t *testing.T) {
	svc, _, gh, r := submitFixture(t)
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, ""); err == nil ||
		!strings.Contains(err.Error(), "nothing to send") {
		t.Fatalf("empty comment review = %v", err)
	}
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictApprove, ""); err != nil {
		t.Fatalf("a bare approval is a valid review: %v", err)
	}
	if gh.submitted[0].Event != EventApprove {
		t.Fatalf("event = %s", gh.submitted[0].Event)
	}
}

func TestSecondRoundSendsOnlyWhatIsNew(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	addHuman(t, st, r.ID, "main.go", 4, SideNew, "round one")
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	addHuman(t, st, r.ID, "main.go", 5, SideNew, "round two")
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	if len(gh.submitted) != 2 || len(gh.submitted[1].Comments) != 1 || gh.submitted[1].Comments[0].Body != "round two" {
		t.Fatalf("second review = %+v", gh.submitted)
	}
}

func TestAFailedSubmissionLeavesEverythingUnsent(t *testing.T) {
	svc, st, gh, r := submitFixture(t)
	gh.submitErr = os.ErrPermission
	addHuman(t, st, r.ID, "main.go", 4, SideNew, "keep me")
	if _, _, err := svc.SubmitReview(context.Background(), r, VerdictComment, "s"); err == nil {
		t.Fatal("expected an error")
	}
	cur, _ := st.Get(r.ID)
	if cur.Posted || cur.Threads[0].Comments[0].Posted || !cur.Threads[0].Comments[0].Pending {
		t.Fatalf("a failed submit marked things sent: %+v", cur.Threads[0].Comments[0])
	}
}

func TestSchema1CardsStillLoadAndSuggestionFieldsRoundTrip(t *testing.T) {
	_, st, _, r := submitFixture(t)
	tid, _ := st.AddThread(r.ID, Thread{File: "main.go", Line: 4, Side: SideNew, Comments: []Comment{
		{Author: "sage", Text: "hmm", Suggested: true, Severity: "warn"}}})
	if err := st.DismissSuggestion(r.ID, tid, 0); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(st.ws)
	if err != nil {
		t.Fatal(err)
	}
	cur, ok := reopened.Get(r.ID)
	c := cur.Threads[0].Comments[0]
	if !ok || !c.Suggested || !c.Dismissed || c.Severity != "warn" {
		t.Fatalf("round trip lost fields: %+v", c)
	}
	// A schema-1 card (no new keys) still loads.
	old := "schema = 1\ntitle = \"old\"\nstatus = \"pending\"\nkind = \"branch\"\nmember = \"api\"\nbase = \"main\"\nhead = \"x\"\nchannel = \"\"\nworktree = \"\"\nposted = false\nviewed = []\ndone = false\n"
	os.WriteFile(filepath.Join(st.dir(), "legacy.toml"), []byte(old), 0o644)
	again, err := Open(st.ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Get("legacy"); !ok {
		t.Fatalf("schema 1 card rejected: %v", again.Warnings())
	}
}

func TestAcceptEditAndDismissSuggestions(t *testing.T) {
	_, st, _, r := submitFixture(t)
	tid, _ := st.AddThread(r.ID, Thread{File: "main.go", Line: 4, Side: SideNew, Comments: []Comment{
		{Author: "sage", Text: "original", Suggested: true, Severity: "nit"},
		{Author: "sage", Text: "second", Suggested: true},
	}})
	if err := st.AcceptSuggestion(r.ID, tid, 0, Human, "edited wording"); err != nil {
		t.Fatal(err)
	}
	cur, _ := st.Get(r.ID)
	c := cur.Threads[0].Comments[0]
	if c.Author != Human || c.Suggested || c.Text != "edited wording" || !c.Pending || c.Severity != "" || !c.Sendable(Human) {
		t.Fatalf("accepted comment = %+v", c)
	}
	if err := st.AcceptSuggestion(r.ID, tid, 0, Human, ""); err == nil {
		t.Fatal("accepting a non-suggestion succeeded")
	}
	if err := st.DismissSuggestion(r.ID, tid, 1); err != nil {
		t.Fatal(err)
	}
	cur, _ = st.Get(r.ID)
	if cur.OpenSuggestions() != 0 || !cur.Threads[0].Comments[1].Dismissed {
		t.Fatalf("after dismiss: %+v", cur.Threads[0].Comments)
	}
	if err := st.DismissSuggestion(r.ID, tid, 0); err == nil {
		t.Fatal("dismissing a human comment succeeded")
	}
}
