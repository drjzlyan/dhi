package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// stubTurner records Handle calls.
type stubTurner struct {
	handled []bus.Message
}

func (s *stubTurner) Handle(_ context.Context, m bus.Message) {
	s.handled = append(s.handled, m)
}

func newPaneFixture(t *testing.T) (*chatPane, *workspace.Workspace, *org.Org, *stubTurner) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	o, err := org.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.CreateTeam("frontend", "", []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	turns := &stubTurner{}
	pane := newChatPane(b, turns, o)
	pane.buildChannels(nil, o.Teams())
	return pane, ws, o, turns
}

func seedAgent(t *testing.T, ws *workspace.Workspace, id string) {
	t.Helper()
	dir := filepath.Join(ws.Root, ".dhi", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "schema = 1\nname = \"" + strings.ToUpper(id[:1]) + id[1:] + "\"\nmodel = \"mock-1\"\n"
	if err := os.WriteFile(filepath.Join(dir, id+".toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildChannelsOrderingAndPersistence(t *testing.T) {
	p, ws, o, _ := newPaneFixture(t)
	seedAgent(t, ws, "alice")
	seedAgent(t, ws, "bob")

	p.buildChannels([]string{"bob", "alice"}, o.Teams())
	got := strings.Join(p.channels, ",")
	want := "#general,#frontend,dm:alice,dm:bob"
	if got != want {
		t.Fatalf("channels = %q, want %q", got, want)
	}
	if p.channelName() != "#general" {
		t.Fatalf("active = %q", p.channelName())
	}

	// Rebuild keeps the active channel selected by name.
	p.active = 2 // dm:alice
	p.refreshRoster([]string{"alice"}, o.Teams())
	if p.channelName() != "dm:alice" {
		t.Fatalf("selection lost: %q", p.channelName())
	}
}

func TestComposerPostsThroughBusAndRuntime(t *testing.T) {
	p, _, _, turns := newPaneFixture(t)
	p.resubscribe()

	p.focus = true
	for _, r := range "@alice ship it" {
		if !p.composerKey(string(r)) {
			t.Fatalf("rune %q rejected", string(r))
		}
	}
	if !p.composerKey("enter") {
		t.Fatal("enter rejected")
	}
	if len(p.input) != 0 {
		t.Fatalf("input not cleared: %q", string(p.input))
	}
	hist := p.bus.History("#general", 0)
	if len(hist) != 1 || hist[0].Text != "@alice ship it" || hist[0].Author != bus.Human {
		t.Fatalf("history = %+v", hist)
	}
	if len(turns.handled) != 1 || turns.handled[0].ID != hist[0].ID {
		t.Fatalf("runtime handled = %+v", turns.handled)
	}

	// Empty enter is a no-op; esc blurs; blurred pane ignores stray keys.
	if !p.composerKey("enter") || len(p.bus.History("#general", 0)) != 1 {
		t.Fatal("empty enter posted or mis-consumed")
	}
	p.composerKey("esc")
	if p.focus {
		t.Fatal("esc did not blur")
	}
	if p.handleKey("x") {
		t.Fatal("blurred pane consumed unrelated key")
	}
}

func TestThreadDrillDownAndReplyTargeting(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.resubscribe()

	root, _ := p.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "plan the thing"})
	reply, _ := p.bus.Post(bus.Message{Channel: "#general", Thread: root.ID,
		Author: "alice", Text: "on it"})
	p.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "separate top-level"})

	// Cursor sits on the first message; open its thread.
	if !p.handleKey("t") || p.threadID != root.ID {
		t.Fatalf("thread = %d", p.threadID)
	}
	vis := p.visibleHistory()
	if len(vis) != 2 || vis[0].ID != root.ID || vis[1].ID != reply.ID {
		t.Fatalf("thread view = %+v", vis)
	}
	_ = vis

	// Reply while drilled down targets the thread.
	p.focus = true
	for _, r := range "hi" {
		p.composerKey(string(r))
	}
	p.composerKey("enter")
	last := p.visibleHistory()
	if n := len(last); n != 3 || last[0].ID != root.ID ||
		last[2].Thread != root.ID || last[2].Text != "hi" {
		t.Fatalf("thread reply = %+v", last)
	}

	// c returns to the top-level channel view (threaded messages stay
	// out of it by design).
	p.focus = false
	p.handleKey("c")
	top := p.visibleHistory()
	if p.threadID != 0 || len(top) != 2 {
		t.Fatalf("back to channel: thread=%d n=%d (%+v)", p.threadID, len(top), top)
	}
	for _, m := range top {
		if m.Thread != 0 {
			t.Fatalf("threaded message leaked into channel view: %+v", m)
		}
	}
}

func TestChannelSwitchResetsCursorAndThread(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.resubscribe()
	m, _ := p.bus.Post(bus.Message{Channel: "#frontend", Author: bus.Human, Text: "hello team"})
	p.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "gen"})

	p.handleKey(".") // → #frontend
	if p.channelName() != "#frontend" || len(p.visibleHistory()) != 1 ||
		p.visibleHistory()[0].ID != m.ID {
		t.Fatalf("switch failed: %q %+v", p.channelName(), p.visibleHistory())
	}
	if !p.handleKey("t") || p.threadID != m.ID {
		t.Fatalf("thread not opened: %d", p.threadID)
	}
	p.cursor = 5     // out-of-range cursor must not survive the next switch
	p.handleKey(",") // back to previous channel
	if p.channelName() != "#general" {
		t.Fatalf("channel = %q", p.channelName())
	}
	if p.threadID != 0 {
		t.Fatalf("switch kept thread drill-down: %d", p.threadID)
	}
	if p.cursor != 0 {
		t.Fatalf("switch kept cursor: %d", p.cursor)
	}
}

func TestCursorClampsToVisibleHistory(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.resubscribe()
	p.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "one"})

	p.cursor = 99
	p.render(80, 24)
	if p.cursor != 0 { // clamped to len(history)-1 during render
		t.Fatalf("cursor = %d, want 0", p.cursor)
	}
}

func TestNilBusPaneInertButRendered(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	p := newChatPane(nil, nil, nil)
	if p.handleKey("i") || p.handleKey(",") || p.handleKey("j") {
		t.Fatal("nil-bus pane consumed keys")
	}
	out := strings.Join(p.render(60, 12), "\n")
	if !strings.Contains(ansi.Strip(out), "no message bus") {
		t.Fatalf("nil-bus render:\n%s", ansi.Strip(out))
	}
}

func TestRailHighlightsActiveChannel(t *testing.T) {
	p, _, o, _ := newPaneFixture(t)
	p.buildChannels([]string{"alice"}, o.Teams())
	p.active = 1 // #frontend
	rail := ansi.Strip(p.rail())
	if !strings.Contains(rail, "[#frontend]") || !strings.Contains(rail, "#general") {
		t.Fatalf("rail = %q", rail)
	}
}

func TestRenderBoundsAndThreadHeader(t *testing.T) {
	p, _, _, _ := newPaneFixture(t)
	p.resubscribe()
	p.bus.Post(bus.Message{Channel: "#general", Author: "alice", Text: strings.Repeat("word ", 40)})
	p.handleKey(".") // #frontend (empty)
	p.handleKey(",") // back
	p.handleKey("t")

	rows := p.render(70, 20)
	joined := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "thread #") {
		t.Fatalf("thread header missing:\n%s", joined)
	}
	for i, l := range rows {
		if w := len([]rune(ansi.Strip(l))); w > 70+2 { // marker prefix tolerance
			t.Fatalf("row %d exceeds width: %d", i, w)
		}
	}
}
