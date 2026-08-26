package reviewer

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/review"
)

// commentOnFirstLine presses c on the first diff line, types text, saves.
func commentOnFirstLine(t *testing.T, m *Model, text string) {
	t.Helper()
	for i := 0; i < 3 && m.sec != secDiff; i++ {
		m.HandleKey("]")
	}
	for i := 0; i < 8; i++ {
		row := m.rowAt(m.cursor)
		if row == nil || row.kind == vrLineUnified && row.left != nil &&
			row.left.NewNo > 0 && row.left.Text != "" {
			break
		}
		m.cursor++
	}
	file, line, _, ok := m.anchorAtCursor()
	if !ok || file != "main.go" || line == 0 {
		t.Fatalf("anchor = %s:%d ok=%v", file, line, ok)
	}
	if !m.HandleKey("c") {
		t.Fatal("composer key not consumed")
	}
	if m.composer == nil {
		t.Fatal("composer did not open")
	}
	typeText(t, m, text)
	m.HandleKey("enter")
	if m.composer != nil {
		t.Fatal("composer did not close")
	}
}

func typeText(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, r := range text {
		if !m.HandleKey(string(r)) {
			t.Fatalf("key %q dropped", string(r))
		}
	}
}

func TestCommentThreadLifecycle(t *testing.T) {
	m, _, st, _ := newSurface(t)
	r := startBranchReview(t, m, st)
	commentOnFirstLine(t, m, "why here?")

	got, _ := st.Get(r.ID)
	if len(got.Threads) != 1 || got.Threads[0].File != "main.go" ||
		got.Threads[0].Comments[0].Text != "why here?" ||
		!got.Threads[0].Comments[0].Pending {
		t.Fatalf("thread = %+v", got.Threads)
	}
	threadID := got.Threads[0].ID

	// thread drill-down lists the pending draft; badge shows in REVIEWS
	m.HandleKey("t")
	out := ansiStrip(m.View())
	if !strings.Contains(out, "threads — main.go") || !strings.Contains(out, "why here?") ||
		!strings.Contains(out, "1 pending") {
		t.Fatalf("thread view wrong:\n%s", out)
	}

	// reply into the thread
	m.HandleKey("a")
	typeText(t, m, "because reasons")
	m.HandleKey("enter")
	got, _ = st.Get(r.ID)
	if len(got.Threads[0].Comments) != 2 {
		t.Fatalf("comments after reply = %+v", got.Threads[0].Comments)
	}

	// resolve toggles from any row of the thread
	m.HandleKey("r")
	got, _ = st.Get(r.ID)
	if !got.Threads[0].Resolved {
		t.Fatal("resolve failed")
	}

	// edit the first pending comment via cursor navigation
	m.threadCur = 1 // first comment row
	m.HandleKey("e")
	if m.composer == nil {
		t.Fatal("edit composer missing")
	}
	m.HandleKey("backspace")
	m.HandleKey("backspace")
	typeText(t, m, "!!")
	m.HandleKey("enter")
	got, _ = st.Get(r.ID)
	if got.Threads[0].Comments[0].Text != "why her!!" {
		t.Errorf("edited text = %q", got.Threads[0].Comments[0].Text)
	}
	_ = threadID

	// delete a draft
	before := len(got.Threads[0].Comments)
	m.HandleKey("x")
	got, _ = st.Get(r.ID)
	if len(got.Threads[0].Comments) != before-1 {
		t.Fatalf("delete failed: %+v", got.Threads[0].Comments)
	}
}

func TestSubmitBatchFromReviews(t *testing.T) {
	m, _, st, _ := newSurface(t)
	r := startBranchReview(t, m, st)
	commentOnFirstLine(t, m, "nit")
	commentOnFirstLine(t, m, "also this")

	m.HandleKey("esc") // → REVIEWS
	m.HandleKey("s")
	got, _ := st.Get(r.ID)
	if got.Status != review.Submitted || got.PendingCount() != 0 {
		t.Fatalf("after submit: status=%s pending=%d", got.Status, got.PendingCount())
	}
	for _, c := range got.Threads[0].Comments {
		if c.Pending {
			t.Fatalf("comment still pending: %+v", c)
		}
	}

	// submitted comments are immutable through the composer too
	m.HandleKey("]")
	m.HandleKey("t")
	m.threadCur = 1  // first comment row
	m.HandleKey("e") // submitted draft must not reopen
	if m.composer != nil {
		t.Fatal("edit opened on a submitted comment")
	}
}

func TestComposerCancelAndEmptySave(t *testing.T) {
	m, _, st, _ := newSurface(t)
	r := startBranchReview(t, m, st)
	m.HandleKey("]")
	m.HandleKey("c")
	if m.composer == nil {
		t.Fatal("no composer")
	}
	m.HandleKey("enter") // empty text ignored
	got, _ := st.Get(r.ID)
	if len(got.Threads) != 0 {
		t.Fatalf("empty comment created thread: %+v", got.Threads)
	}
	m.HandleKey("x")
	m.HandleKey("esc")
	if m.composer != nil {
		t.Fatal("esc did not cancel composer")
	}
	got, _ = st.Get(r.ID)
	if len(got.Threads) != 0 {
		t.Fatal("cancelled composer persisted data")
	}
}
