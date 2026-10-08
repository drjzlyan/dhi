package reviewer

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/review"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// screenModel is gapModel with a thread on main.go, sized for the screen.
func screenModel(t *testing.T, w int) *Model {
	t.Helper()
	m := gapModel(t, true)
	m.sec = secDiff
	m.Resize(w, 32)
	r, _ := m.openReview()
	th := review.Thread{File: "main.go", Line: 4, Side: review.SideNew,
		Comments: []review.Comment{{Author: review.Human, Text: "why uppercase?", Pending: true, Side: review.SideNew, Line: 4}}}
	if _, err := m.svc.Store().AddThread(r.ID, th); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReviewScreenNeedsAWideTerminal(t *testing.T) {
	if m := screenModel(t, kit.WWide-1); m.screenMode() {
		t.Fatal("three columns below the wide threshold")
	}
	m := screenModel(t, kit.WWide)
	if !m.screenMode() {
		t.Fatal("no three-column screen at the wide threshold")
	}
	m.sec = secFiles
	if m.screenMode() {
		t.Fatal("screen shown outside the diff section")
	}
}

func TestReviewScreenShowsFilesDiffAndConversation(t *testing.T) {
	m := screenModel(t, 140)
	out := ansiStrip(m.View())
	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Errorf("screen is %d rows, want %d", len(lines), m.height)
	}
	for _, want := range []string{"files", "diff", "conversation", "main.go", "LINE4", "why uppercase?", "viewed"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen lacks %q:\n%s", want, out)
		}
	}
	golden.Snapshot(t, "reviewer-screen-wide", out)
}

func TestReviewScreenKeysStillWork(t *testing.T) {
	m := screenModel(t, 140)
	m.HandleKey("t")
	if !m.threadOpen {
		t.Fatal("t did not open threads inside the screen")
	}
	if !strings.Contains(ansiStrip(m.View()), "threads —") {
		t.Fatal("thread view missing from the diff column")
	}
}

func TestClickingAFileJumpsTheDiff(t *testing.T) {
	m := screenModel(t, 140)
	m.files = gitdiff.Parse(twoHunkPatch + `diff --git a/util.go b/util.go
new file mode 100644
--- /dev/null
+++ b/util.go
@@ -0,0 +1,2 @@
+package util
+
`)
	m.resetGapState()
	_ = m.View()
	if m.Click(screenFilesW+5, 5) {
		t.Fatal("click in the diff column was swallowed")
	}
	if !m.Click(2, 1+m.screenRows+1) || m.fileCur != 1 {
		t.Fatalf("click on the second file: fileCur=%d", m.fileCur)
	}
	if row := m.rowAt(m.cursor); row == nil || row.kind != vrFileHeader || row.file != 1 {
		t.Fatalf("diff cursor did not jump to util.go: %+v", row)
	}
}
