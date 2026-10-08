package reviewer

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/review"
)

// agentAnswer posts an employee's reply to the review's channel and lets the
// surface mirror it, exactly as the runtime would.
func agentAnswer(t *testing.T, m *Model, b *bus.Bus, r review.Review, author, text string) {
	t.Helper()
	posted, err := b.Post(bus.Message{Channel: r.Channel, Author: author, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	m.mirrorBus(posted)
}

const findingsReply = "Here is my review:\n```json\n{\"summary\":\"Two things.\",\"findings\":[" +
	"{\"file\":\"main.go\",\"line\":4,\"side\":\"new\",\"severity\":\"warn\",\"comment\":\"println does not belong here\"}," +
	"{\"file\":\"main.go\",\"line\":1,\"severity\":\"nit\",\"comment\":\"missing package comment\"}]}\n```"

func openThreadsFor(m *Model, file string) {
	m.sec = secDiff
	m.threadOpen = true
	m.threadFile = file
	m.threadCur = 0
}

func findSuggestionRow(t *testing.T, m *Model, text string) int {
	t.Helper()
	r, _ := m.openReview()
	rows, order := flatThreads(r, m.threadFile)
	for i, tr := range rows {
		if tr.comment >= 0 && strings.Contains(order[tr.thread].Comments[tr.comment].Text, text) {
			return i
		}
	}
	t.Fatalf("no row containing %q among %d rows", text, len(rows))
	return -1
}

func TestStructuredAnswerBecomesAnchoredSuggestionsNotComments(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	agentAnswer(t, m, b, r, "rev", findingsReply)

	cur, _ := st.Get(r.ID)
	if cur.OpenSuggestions() != 3 { // summary + 2 findings
		t.Fatalf("suggestions = %d: %+v", cur.OpenSuggestions(), cur.Threads)
	}
	var anchored *review.Thread
	for i := range cur.Threads {
		if cur.Threads[i].Line == 4 {
			anchored = &cur.Threads[i]
		}
	}
	if anchored == nil || anchored.File != "main.go" || !anchored.Comments[0].Suggested ||
		anchored.Comments[0].Author != "rev" || anchored.Comments[0].Severity != "warn" {
		t.Fatalf("anchored = %+v", anchored)
	}
	if cur.PendingCount() != 0 {
		t.Fatal("a suggestion must not count as one of the human's drafts")
	}
	for _, th := range cur.Threads {
		for _, c := range th.Comments {
			if c.Sendable(review.Human) {
				t.Fatalf("a suggestion is sendable before the human decides: %+v", c)
			}
		}
	}
}

func TestUnstructuredAnswerIsKeptAsOneReviewLevelSuggestion(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	agentAnswer(t, m, b, r, "rev", "overall LGTM")
	cur, _ := st.Get(r.ID)
	if len(cur.Threads) != 1 || cur.Threads[0].File != review.SummaryFile ||
		!cur.Threads[0].Comments[0].Suggested || cur.Threads[0].Comments[0].Text != "overall LGTM" {
		t.Fatalf("threads = %+v", cur.Threads)
	}
}

func TestRetryNoticesAreNotSuggestions(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	agentAnswer(t, m, b, r, "rev", "retrying (attempt 1/2) in 30s…")
	if cur, _ := st.Get(r.ID); len(cur.Threads) != 0 {
		t.Fatalf("a status line became a finding: %+v", cur.Threads)
	}
}

func TestAcceptKeyMakesItTheHumansOwnDraft(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	agentAnswer(t, m, b, r, "rev", findingsReply)
	openThreadsFor(m, "main.go")
	m.threadCur = findSuggestionRow(t, m, "println does not belong")
	if !m.threadsKey("y") {
		t.Fatal("y not handled")
	}
	cur, _ := st.Get(r.ID)
	var got review.Comment
	for _, th := range cur.Threads {
		for _, c := range th.Comments {
			if strings.Contains(c.Text, "println does not belong") {
				got = c
			}
		}
	}
	if got.Author != review.Human || got.Suggested || !got.Pending || !got.Sendable(review.Human) {
		t.Fatalf("accepted = %+v", got)
	}
	if cur.OpenSuggestions() != 2 {
		t.Fatalf("open suggestions = %d, want the other 2", cur.OpenSuggestions())
	}
}

func TestEditKeyOpensTheSuggestionAndSavingAcceptsTheEditedText(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	agentAnswer(t, m, b, r, "rev", findingsReply)
	openThreadsFor(m, "main.go")
	m.threadCur = findSuggestionRow(t, m, "missing package comment")
	m.threadsKey("e")
	if m.composer == nil || !m.composer.accept || string(m.composer.runes) != "missing package comment" {
		t.Fatalf("composer = %+v", m.composer)
	}
	for range "missing package comment" {
		m.composerKey("backspace")
	}
	for _, r := range "Add a package doc comment." {
		m.composerKey(string(r))
	}
	m.composerKey("enter")
	cur, _ := st.Get(r.ID)
	found := false
	for _, th := range cur.Threads {
		for _, c := range th.Comments {
			if c.Text == "Add a package doc comment." {
				found = true
				if c.Author != review.Human || c.Suggested || !c.Sendable(review.Human) {
					t.Fatalf("edited+accepted = %+v", c)
				}
			}
		}
	}
	if !found {
		t.Fatalf("edited text not saved: %+v", cur.Threads)
	}
}

func TestDismissHidesTheSuggestionAndItNeverGoesOut(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	agentAnswer(t, m, b, r, "rev", findingsReply)
	openThreadsFor(m, "main.go")
	row := findSuggestionRow(t, m, "println does not belong")
	m.threadCur = row
	m.threadsKey("x")
	cur, _ := st.Get(r.ID)
	rows, order := flatThreads(cur, "main.go")
	for _, tr := range rows {
		if tr.comment >= 0 && strings.Contains(order[tr.thread].Comments[tr.comment].Text, "println does not belong") {
			t.Fatal("a dismissed suggestion is still listed")
		}
	}
	if cur.OpenSuggestions() != 2 {
		t.Fatalf("open = %d", cur.OpenSuggestions())
	}
}

func TestThreadsViewLabelsSuggestionsAndCountsThem(t *testing.T) {
	m, st, b, _ := newAgentSurface(t)
	r := startBranchReview(t, m, st)
	agentAnswer(t, m, b, r, "rev", findingsReply)
	openThreadsFor(m, "main.go")
	out := ansi.Strip(m.renderThreads(110, 30))
	for _, want := range []string{"rev suggests (warn)", "println does not belong",
		"suggestion(s) from your team await your decision", "y accept"} {
		if !strings.Contains(out, want) {
			t.Errorf("threads view lacks %q:\n%s", want, out)
		}
	}
}

func TestReviewRequestAsksForStructuredFindings(t *testing.T) {
	m, st, _, fc := newAgentSurface(t)
	startBranchReview(t, m, st)
	m.HandleKey("A")
	m.submitForm()
	_ = m.Update(pumpCmd(t, m.listen()))
	calls := fc.calls()
	if len(calls) != 1 {
		t.Fatal("not dispatched")
	}
	for _, want := range []string{"```json", "\"findings\"", "\"severity\"", "empty findings list", "```diff"} {
		if !strings.Contains(calls[0].Text, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}
