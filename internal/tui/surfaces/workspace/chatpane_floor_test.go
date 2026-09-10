package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
)

// seedFloor posts a channel conversation with one threaded reply.
func seedFloor(p *chatPane) {
	root, _ := p.bus.Post(bus.Message{Channel: "#general", Author: "you",
		Text: "please review the login flow"})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Thread: root.ID, Text: "on it — found a race in the token refresh"})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "@you summary posted to the board card"})
}

func TestWideFloorThreeColumns(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	seedFloor(p)
	p.buildChannels([]string{"scout"}, p.org.Teams())

	rows := p.render(110, 20)
	if len(rows) != 20 {
		t.Fatalf("rows = %d, want height", len(rows))
	}
	joined := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"CHANNELS", "DIRECT MESSAGES", "#general", "dm:scout", "#frontend"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("rail missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "please review the login flow") {
		t.Fatalf("transcript missing:\n%s", joined)
	}
	for y, row := range rows {
		if w := len([]rune(ansi.Strip(row))); w != 110 {
			t.Fatalf("row %d width = %d, want 110", y, w)
		}
	}
}

func TestWideThreadPaneBesideTranscript(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	seedFloor(p)
	p.buildChannels([]string{"scout"}, p.org.Teams())
	p.render(110, 20) // register wide mode

	// Cursor on the root message, then t opens the side pane.
	if !p.handleKey("t") || p.threadID == 0 {
		t.Fatal("t did not open the thread")
	}
	rows := p.render(110, 20)
	joined := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "thread #") {
		t.Fatalf("thread pane missing:\n%s", joined)
	}
	// Replies render word-wrapped in the pane.
	if !strings.Contains(joined, "on it") || !strings.Contains(joined, "token") {
		t.Fatalf("thread replies missing:\n%s", joined)
	}
	// The transcript still shows the channel, not just the thread.
	if !strings.Contains(joined, "@you summary posted") {
		t.Fatalf("transcript lost the channel view:\n%s", joined)
	}
	// esc closes the pane.
	if !p.handleKey("esc") || p.threadID != 0 {
		t.Fatal("esc did not close the thread pane")
	}
}

func TestWideProfilePane(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	seedFloor(p)
	p.buildChannels([]string{"scout"}, p.org.Teams())
	calls := 0
	p.profile = func(id string) []string {
		calls++
		return []string{"model mock-1", "teams frontend"}
	}

	// v on an agent-authored message opens the profile pane.
	p.handleKey("j") // cursor to scout's channel message
	if !p.handleKey("v") || p.profileID != "scout" {
		t.Fatal("v did not open the profile")
	}
	joined := ansi.Strip(strings.Join(p.render(110, 20), "\n"))
	if !strings.Contains(joined, "model mock-1") || calls != 1 {
		t.Fatalf("profile pane missing (calls=%d):\n%s", calls, joined)
	}
	p.handleKey("esc")
	if p.profileID != "" {
		t.Fatal("esc did not close the profile")
	}
}

func TestWideRailNavigation(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.buildChannels([]string{"scout"}, p.org.Teams())
	p.render(110, 20) // register wide mode

	if !p.handleKey("tab") || !p.railFocus {
		t.Fatal("tab did not focus the rail")
	}
	if !p.handleKey("j") || p.railCur != 1 { // #frontend
		t.Fatalf("rail cursor = %d", p.railCur)
	}
	if !p.handleKey("enter") || p.channelName() != "#frontend" {
		t.Fatal("enter did not open the highlighted channel")
	}
	if p.railFocus {
		t.Fatal("opening a channel must return focus to the transcript")
	}
	// Opening marks read (F-017).
	if p.unreadCount("#frontend") != 0 {
		t.Fatal("opened channel not marked read")
	}
}

func TestOpenAtLandsThreadPane(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	seedFloor(p)
	p.buildChannels([]string{"scout"}, p.org.Teams())
	p.render(110, 20) // wide mode registers

	var mention bus.Message
	for _, m := range p.bus.History("#general", 0) {
		if m.Author == "scout" {
			mention = m
		}
	}
	if !p.openAt("#general", 0, mention.ID) {
		t.Fatal("openAt failed")
	}
	if p.cursor == 0 {
		t.Fatal("openAt did not position the cursor")
	}
}

func TestChannelsFloorGolden(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	seedFloor(p)
	p.buildChannels([]string{"scout"}, p.org.Teams())
	p.render(110, 22)
	root, _ := p.bus.Post(bus.Message{Channel: "#general", Author: "you",
		Text: "please review the login flow"})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Thread: root.ID, Text: "on it — found a race in the token refresh"})
	p.handleKey("k") // cursor to the root
	p.handleKey("t") // open the thread pane
	golden.Snapshot(t, "workspace_channels_floor", strings.Join(p.render(110, 22), "\n"))
}

func TestChannelsProfileGolden(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	seedFloor(p)
	p.buildChannels([]string{"scout"}, p.org.Teams())
	p.profile = func(id string) []string {
		return []string{"model mock-1", "teams frontend", "tasks 1 open · 0 done"}
	}
	p.render(110, 22)
	p.handleKey("v")
	golden.Snapshot(t, "workspace_channels_profile", strings.Join(p.render(110, 22), "\n"))
}
