package reviewer

import (
	"slices"
	"testing"

	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tutorial"
)

func TestReviewerReportsOpenCommentAndSubmit(t *testing.T) {
	m, st, _ := prFixture(t)
	var got []string
	m.SetEmitter(func(ev string) { got = append(got, ev) })

	m.open("api-pr-42")
	if !slices.Contains(got, tutorial.EvReviewOpened) {
		t.Fatalf("opening a review reported %v", got)
	}

	// An empty draft is ignored: no event.
	m.composer = &composer{file: "main.go", line: 2, side: review.SideNew, editIdx: -1}
	m.HandleKey("enter")
	if slices.Contains(got, tutorial.EvReviewComment) {
		t.Fatalf("an empty draft reported %v", got)
	}
	m.composer = &composer{file: "main.go", line: 2, side: review.SideNew, editIdx: -1, runes: []rune("why?")}
	m.HandleKey("enter")
	if !slices.Contains(got, tutorial.EvReviewComment) {
		t.Fatalf("saving a comment reported %v", got)
	}
	if cur, _ := st.Get("api-pr-42"); cur.PendingCount() != 1 {
		t.Fatalf("the draft was not saved: %d pending", cur.PendingCount())
	}

	m.HandleKey("S")
	m.HandleKey("enter")
	m.HandleKey("enter")
	// open() queued a diff load and a remote sync ahead of the submit result.
	for i := 0; i < 6 && !slices.Contains(got, tutorial.EvReviewSubmitted) && m.opErr == ""; i++ {
		pumpSubmit(t, m)
	}
	if !slices.Contains(got, tutorial.EvReviewSubmitted) {
		t.Fatalf("sending the review reported %v (err=%q)", got, m.opErr)
	}
}

func TestAFailedSubmitReportsNothing(t *testing.T) {
	m, _, fgh := prFixture(t)
	fgh.login, fgh.prAuthor = "me", "me" // approving your own PR is refused
	var got []string
	m.SetEmitter(func(ev string) { got = append(got, ev) })
	m.HandleKey("S")
	m.form.fields[0].val = 1
	m.HandleKey("enter")
	m.HandleKey("enter")
	pumpSubmit(t, m)
	if m.opErr == "" {
		t.Fatal("the refusal never arrived")
	}
	if slices.Contains(got, tutorial.EvReviewSubmitted) {
		t.Fatalf("a refused review reported %v", got)
	}
}
