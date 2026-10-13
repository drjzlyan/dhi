package workspace

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/unread"
)

func f072Pane(t *testing.T) *chatPane {
	t.Helper()
	p, _, _, _ := newPaneFixture(t)
	p.buildChannels([]string{"scout", "lead"}, p.org.Teams())
	return p
}

func TestStreamLinesStayOutOfChat(t *testing.T) {
	p := f072Pane(t)
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout fix it"})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Text: "bash: go test ./...", Kind: bus.KindStream})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Text: "Fixed the remainder bug.", Kind: bus.KindReply})
	plain := ansi.Strip(strings.Join(p.render(110, 16), "\n"))
	if strings.Contains(plain, "go test") {
		t.Fatalf("tool line leaked into chat:\n%s", plain)
	}
	if !strings.Contains(plain, "Fixed the remainder bug.") {
		t.Fatalf("reply missing:\n%s", plain)
	}
	if n := len(p.visibleHistory()); n != 2 {
		t.Fatalf("visible history = %d, want 2", n)
	}
	// Stream lines never need the human.
	if unread.AddressedToHuman(bus.Message{Channel: "dm:scout", Author: "scout", Kind: bus.KindStream}) {
		t.Fatal("a stream line counted as attention")
	}
	if !unread.AddressedToHuman(bus.Message{Channel: "#general", Author: "scout", Kind: bus.KindAlert}) {
		t.Fatal("an alert must count as attention")
	}
}

func TestStatusLineWhileWorking(t *testing.T) {
	p := f072Pane(t)
	working := true
	p.agentWorking = func(id string) bool { return working && id == "scout" }
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout fix it"})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Text: "bash: go test ./...", Kind: bus.KindStream})
	if got := ansi.Strip(p.statusLine()); !strings.Contains(got, "scout is running tests") {
		t.Fatalf("status line = %q", got)
	}
	plain := ansi.Strip(strings.Join(p.render(110, 16), "\n"))
	if !strings.Contains(plain, "scout is running tests…") {
		t.Fatalf("status line not rendered:\n%s", plain)
	}
	// The reply lands: the indicator clears even before presence does.
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Text: "Done.", Kind: bus.KindReply})
	if got := p.statusLine(); got != "" {
		t.Fatalf("status after reply = %q", ansi.Strip(got))
	}
	working = false
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Text: "command: ls", Kind: bus.KindStream})
	if got := p.statusLine(); got != "" {
		t.Fatalf("idle agent shows status %q", ansi.Strip(got))
	}
}

func TestRailGroupsNamesAndPresence(t *testing.T) {
	p := f072Pane(t)
	p.agentWorking = func(id string) bool { return id == "scout" }
	plain := ansi.Strip(strings.Join(p.railLines(slackRailW, 12), "\n"))
	if strings.Contains(plain, "dm:") {
		t.Fatalf("rail shows dm: prefix:\n%s", plain)
	}
	for _, want := range []string{"CHANNELS", "#general", "DIRECT MESSAGES", "● scout", "○ lead"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rail missing %q:\n%s", want, plain)
		}
	}
	if strings.Index(plain, "DIRECT MESSAGES") < strings.Index(plain, "#frontend") {
		t.Fatalf("DM header above channels:\n%s", plain)
	}
}

func TestRailFilterFindsByName(t *testing.T) {
	p := f072Pane(t)
	p.render(110, 16)
	p.handleKey("tab") // rail focus
	p.handleKey("/")
	for _, k := range []string{"s", "c", "o"} {
		p.handleKey(k)
	}
	if list := p.railList(); len(list) != 1 || list[0] != "dm:scout" {
		t.Fatalf("filtered rail = %v", list)
	}
	p.handleKey("enter")
	if p.channelName() != "dm:scout" {
		t.Fatalf("enter opened %q", p.channelName())
	}
	if p.railQuery != "" || p.railSearching {
		t.Fatal("filter not cleared after opening")
	}
}

func TestQuickSwitcher(t *testing.T) {
	p := f072Pane(t)
	p.render(110, 16)
	if !p.handleKey("ctrl+g") || p.switcher == nil || !p.capturing() {
		t.Fatal("ctrl+g did not open the switcher")
	}
	for _, k := range []string{"f", "r", "o"} {
		p.handleKey(k)
	}
	plain := ansi.Strip(strings.Join(p.render(110, 16), "\n"))
	if !strings.Contains(plain, "JUMP TO A CHANNEL OR DM") || !strings.Contains(plain, "#frontend") {
		t.Fatalf("switcher not rendered:\n%s", plain)
	}
	p.handleKey("enter")
	if p.switcher != nil || p.channelName() != "#frontend" {
		t.Fatalf("switcher open=%v channel=%q", p.switcher != nil, p.channelName())
	}
	p.handleKey("ctrl+g")
	p.handleKey("esc")
	if p.switcher != nil {
		t.Fatal("esc did not close the switcher")
	}
}

func TestAlertRendersAsSystemLine(t *testing.T) {
	p := f072Pane(t)
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Kind: bus.KindAlert,
		Text: "I couldn't finish: the budget is reached."})
	plain := ansi.Strip(strings.Join(p.render(110, 12), "\n"))
	if !strings.Contains(plain, "I couldn't finish: the budget is reached.") {
		t.Fatalf("alert missing:\n%s", plain)
	}
}

func TestMarkdownInChatAndThread(t *testing.T) {
	p := f072Pane(t)
	root, _ := p.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "how do I split money?"})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Thread: root.ID, Kind: bus.KindReply,
		Text: "Use the remainder:\n\n```go\nq, r := c/n, c%n\n```"})
	_, _ = p.bus.Post(bus.Message{Channel: "#general", Author: "scout", Kind: bus.KindReply,
		Text: "Steps:\n\n- run `make verify`\n- open the PR"})
	p.render(110, 20)
	plain := ansi.Strip(strings.Join(p.render(110, 20), "\n"))
	if strings.Contains(plain, "`") {
		t.Fatalf("raw markdown in transcript:\n%s", plain)
	}
	if !strings.Contains(plain, "• run make verify") || !strings.Contains(plain, "↳ 1 reply") {
		t.Fatalf("formatted list / reply count missing:\n%s", plain)
	}
	p.cursor = 0
	p.handleKey("t")
	view := strings.Join(p.render(110, 20), "\n")
	plain = ansi.Strip(view)
	if !strings.Contains(plain, "q, r := c/n, c%n") || strings.Contains(plain, "```") {
		t.Fatalf("thread code block not formatted:\n%s", plain)
	}
	for _, tk := range []theme.Tokens{theme.Dark(), theme.Light(), theme.HighContrast()} {
		theme.SwapForTest(t, tk)
		golden.AssertBgIntegrity(t, "chat markdown "+tk.Name, strings.Join(p.render(110, 20), "\n"))
	}
}
