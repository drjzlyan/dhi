package reviewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/tasks"
)

func TestReviewerTranscriptView(t *testing.T) {
	m, ws, st, _ := newSurface(t)

	// A task bound to a task/<slug> branch with one recorded run whose
	// transcript file exists.
	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	ts.Create("feat-1", "Feat", "", "")
	tf := filepath.Join(t.TempDir(), "run.jsonl")
	body := `{"kind":"command","detail":"go test ./..."}` + "\n" +
		`{"kind":"final","detail":"ok"}` + "\n"
	if err := os.WriteFile(tf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := ts.RecordRun("feat-1", tasks.Run{
		ID: "run-1", Agent: "rev", Runtime: "cli:claude", Model: "sonnet",
		Status: tasks.RunOK, Transcript: tf, Started: now, Finished: now,
	}); err != nil {
		t.Fatal(err)
	}
	m.taskStore = ts

	rv := review.Review{ID: "task-feat-1", Title: "feat", Status: review.Pending,
		Target: review.Target{Kind: review.KindBranch, Member: "api", Base: "master", Head: "task/feat-1"}}
	if err := st.Create(rv); err != nil {
		t.Fatal(err)
	}
	m.openID = "task-feat-1"
	m.sec = secDiff

	if !m.HandleKey("T") {
		t.Fatal("T refused")
	}
	if !m.transcriptOpen {
		t.Fatalf("transcript not open: opErr=%q", m.opErr)
	}
	out := ansiStrip(m.activeSectionFor(120, 30))
	for _, want := range []string{"run-1", "cli:claude", "sonnet", "go test ./...", "ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript missing %q:\n%s", want, out)
		}
	}
	// T toggles it back off.
	if !m.HandleKey("T") || m.transcriptOpen {
		t.Fatal("T did not close the transcript")
	}
}

func TestReviewerTranscriptRefusals(t *testing.T) {
	m, _, st, _ := newSurface(t)
	m.sec = secDiff

	// No review open.
	m.toggleTranscript()
	if m.transcriptOpen || !strings.Contains(m.opErr, "no review open") {
		t.Fatalf("no open review: open=%v opErr=%q", m.transcriptOpen, m.opErr)
	}

	// Review not bound to a task branch.
	if err := st.Create(review.Review{ID: "plain-1", Title: "plain", Status: review.Pending,
		Target: review.Target{Kind: review.KindBranch, Member: "api", Base: "master", Head: "feature/x"}}); err != nil {
		t.Fatal(err)
	}
	m.openID = "plain-1"
	m.opErr = ""
	m.toggleTranscript()
	if m.transcriptOpen || !strings.Contains(m.opErr, "not bound to a task branch") {
		t.Fatalf("non-task review: open=%v opErr=%q", m.transcriptOpen, m.opErr)
	}
}
