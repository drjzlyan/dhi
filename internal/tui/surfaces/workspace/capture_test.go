package workspace

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
)

var _ surfaces.InputCapturer = (*Model)(nil)

func TestWorkspaceCapturesInputInFormsFilterAndComposer(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	m.sec = secBoard
	if m.CapturesInput() {
		t.Fatal("browsing the board is navigation")
	}
	m.HandleKey("n") // new-task form
	if !m.CapturesInput() {
		t.Fatal("an open form takes plain keys as text")
	}
	m.HandleKey("esc")
	if m.CapturesInput() {
		t.Fatal("closing the form hands keys back")
	}
	m.HandleKey("/") // board filter
	if !m.CapturesInput() {
		t.Fatal("the board filter is a text prompt")
	}
}

func TestWorkspaceChannelComposerCaptures(t *testing.T) {
	m, _, _ := newSurfaceWithBus(t)
	m.sec = secChannels
	if m.CapturesInput() {
		t.Fatal("a blurred composer must not capture")
	}
	m.pane.focus = true
	if !m.CapturesInput() {
		t.Fatal("a focused composer takes digits and ? as text")
	}
	m.pane.focus, m.pane.searching = false, true
	if !m.CapturesInput() {
		t.Fatal("message search is a text prompt")
	}
}

func TestRailClickJumpsToSectionAndClosesReplay(t *testing.T) {
	m, _ := newSurface(t)
	m.Resize(110, 30)
	m.sec = secBoard
	if !m.Click(3, int(secRepos)) {
		t.Fatal("a click on a rail row should be handled")
	}
	if m.sec != secRepos {
		t.Fatalf("sec = %v, want repos", m.sec)
	}
	if m.Click(60, 2) {
		t.Fatal("a click in the pane is not a rail click")
	}
	m.Resize(70, 30)
	if m.Click(3, 0) {
		t.Fatal("narrow layout has no rail to click")
	}
}
