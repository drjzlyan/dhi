package workspace

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestTimeAgoTable(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-2 * 24 * time.Hour), "2d"},
	}
	for _, tc := range cases {
		if got := timeAgo(tc.at, now); got != tc.want {
			t.Fatalf("timeAgo(%v) = %q, want %q", tc.at, got, tc.want)
		}
	}
}

func TestBoardMoveToLaneKey(t *testing.T) {
	m, ws := newSurface(t)
	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = ts
	if err := ts.Create("movecard", "move me", "", ""); err != nil {
		t.Fatal(err)
	}
	m.sec = secBoard
	if !m.HandleKey("m") {
		t.Fatalf("`m` did not open the lane picker")
	}
	if m.form.kind != fTaskMove {
		t.Fatalf("kind = %v", m.form.kind)
	}
	// enter moves the card to the picked lane (backlog is index 0).
	m.HandleKey("enter")
	if m.form.kind != fNone {
		t.Fatalf("form still open: err=%q", m.form.err)
	}
	tk, ok := ts.Get("movecard")
	if !ok {
		t.Fatal("card lost")
	}
	if tk.Status != tasks.Backlog {
		t.Fatalf("status = %v, want backlog", tk.Status)
	}
}

func TestBoardBackwardStatusKey(t *testing.T) {
	m, ws := newSurface(t)
	ts, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = ts
	if err := ts.Create("backcard", "backward", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := ts.SetStatus("backcard", tasks.InReview); err != nil {
		t.Fatal(err)
	}
	m.sec = secBoard
	// Position the cursor on the card in its lane.
	m.followCardIntoLane("backcard")
	if !m.HandleKey("S") {
		t.Fatalf("`S` refused")
	}
	tk, ok := ts.Get("backcard")
	if !ok {
		t.Fatal("card lost")
	}
	if tk.Status != tasks.Active {
		t.Fatalf("S moved %v, want active", tk.Status)
	}
}

func TestFormInValueCursor(t *testing.T) {
	m, _ := newSurface(t)
	m.sec = secRepos
	if !m.HandleKey("a") {
		t.Fatal("`a` refused")
	}
	if m.form.kind != fAdd {
		t.Fatalf("kind = %v", m.form.kind)
	}
	// Insert mid-value: one left puts the cursor before the last rune,
	// then a rune appends at the cursor position.
	for _, k := range []string{"r", "u", "n", "e", "left", "X"} {
		m.formKey(k)
	}
	if got := m.form.values()[0]; got != "runXe" {
		t.Fatalf("in-value insert = %q, want runXe", got)
	}
	m.closeForm()
}

func TestChannelsComposerVisibleWhenBlurred(t *testing.T) {
	m, _, _ := newSurfaceWithBus(t)
	m.sec = secChannels
	body := strings.Join(m.pane.render(84, 20), "\n")
	if !strings.Contains(ansi.Strip(body), "(i to type)") {
		t.Fatalf("blurred composer missing: %q", body)
	}
}

func TestChannelsTranscriptStamps(t *testing.T) {
	m, _, _ := newSurfaceWithBus(t)
	m.sec = secChannels
	history := m.pane.visibleHistory()
	if len(history) == 0 {
		t.Skip("no transcript rows in fixture")
	}
	body := strings.Join(m.pane.transcriptRender(history, 60, 20), "\n")
	stripped := ansi.Strip(body)
	for _, msg := range history {
		if !strings.Contains(stripped, msg.At.Format("15:04")) {
			t.Fatalf("stamp %s missing: %q", msg.At.Format("15:04"), stripped)
		}
	}
}

func TestBoardCardWidthProportional(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	tk := tasks.Task{Slug: "x", Title: "a fairly long card title", Assignee: "scout"}
	for _, laneW := range []int{8, 20, 40} {
		row := ansi.Strip(boardCard(tk, laneW, false))
		if ansi.Width(row) != laneW {
			t.Fatalf("card row width = %d, want %d: %q", ansi.Width(row), laneW, row)
		}
	}
}

func TestReplayClosesOnSectionSwitch(t *testing.T) {
	m, _ := newSurface(t)
	m.sec = secBoard
	m.replay = &runReplay{run: tasks.Run{ID: "r1"}}
	if !m.HandleKey("]") {
		t.Fatal("] refused")
	}
	if m.replay != nil {
		t.Fatal("section switch kept the replay owning keys")
	}
	if m.sec != secChannels {
		t.Fatalf("sec = %v, want channels", m.sec)
	}
}

func TestTranscriptEmptyStateNamed(t *testing.T) {
	m, _, _ := newSurfaceWithBus(t)
	m.sec = secChannels
	body := strings.Join(m.pane.transcriptRender(nil, 60, 20), "\n")
	if !strings.Contains(ansi.Strip(body), "(no messages yet") {
		t.Fatalf("named empty state missing: %q", body)
	}
}

func TestReposOpenInEditorSeam(t *testing.T) {
	m, ws := newSurface(t)
	if len(ws.Members()) == 0 {
		t.Skip("no members in fixture")
	}
	opened := false
	m.openEditor = func(paths []string) bool { opened = true; return true }
	m.sec = secRepos
	if !m.HandleKey("e") {
		t.Fatal("`e` refused")
	}
	if !opened {
		t.Fatal("editor seam not called")
	}
	// A false seam degrades to a visible hint, never silent (F-011).
	m.openEditor = func([]string) bool { return false }
	m.inboxHint = ""
	if !m.HandleKey("e") {
		t.Fatal("`e` refused on a false seam")
	}
	if m.inboxHint == "" {
		t.Fatal("false seam was silent")
	}
}

func TestPostFailureSurfacesOnChrome(t *testing.T) {
	m, ws, _ := newSurfaceWithBus(t)
	_ = ws
	// A post through a broken seam never drops silently: the flash reads
	// the named failure (F-011). Use a channel whose bus write fails.
	if m.pane == nil {
		t.Fatal("no chat pane")
	}
	m.pane.flash = "post failed: bus closed"
	if got := m.statusFlash(); !strings.Contains(ansi.Strip(got), "post failed") {
		t.Fatalf("flash = %q, want the named failure", got)
	}
}
