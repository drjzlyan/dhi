package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/provider"
	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const scoutDoc = `schema = 1
name = "Scout"
model = "mock-1"
system = "You scout."
tools = ["read", "write"]
policy_json = """{"rules":[{"op":"read","path":"**","effect":"allow"},{"op":"write","path":"docs/**","effect":"ask"}]}"""
`

type chatHarness struct {
	m     *Model
	rt    *runtime.Runtime
	mock  *provider.Mock
	apprs *tools.Approvals
	ws    *workspace.Workspace
}

func newChatEditor(t *testing.T) *chatHarness {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	ws, _ := setupWorkspace(t)
	mock := provider.NewMock()
	ap := tools.NewApprovals()

	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	mf, err := manifest.Parse("scout", []byte(scoutDoc))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	rt, err := runtime.New(runtime.Config{
		WS:        ws,
		Bus:       b,
		Approvals: ap,
		Provider:  mock,
	}, []*manifest.Agent{mf})
	if err != nil {
		t.Fatal(err)
	}
	m := New("test", ws, WithChat(rt))
	m.Resize(120, 30)
	return &chatHarness{m: m, rt: rt, mock: mock, apprs: ap, ws: ws}
}

func (h *chatHarness) openFocused() {
	h.m.HandleKey("ctrl+a")
}

func TestChatToggleTriState(t *testing.T) {
	h := newChatEditor(t)
	if strings.Contains(plainView(h.m), "crew") {
		t.Fatal("sidebar visible before toggle")
	}
	h.openFocused()
	if !strings.Contains(plainView(h.m), "crew") {
		t.Fatal("sidebar missing after ctrl+a")
	}
	golden.Snapshot(t, "editor_chat_focused_120x30", plainView(h.m))
	// focused: typing feeds the input
	typeKeys(h.m, "hi")
	h.m.HandleKey("esc")
	if !strings.Contains(plainView(h.m), "focus input") {
		t.Fatal("esc should blur, not close")
	}
	golden.Snapshot(t, "editor_chat_blurred_120x30", plainView(h.m))
	h.m.HandleKey("ctrl+a")
	if strings.Contains(plainView(h.m), "crew") {
		t.Fatal("second ctrl+a should close")
	}
}

func TestChatSendPostsToBus(t *testing.T) {
	h := newChatEditor(t)
	h.openFocused()
	typeKeys(h.m, "@scout please look")
	h.m.HandleKey("enter")
	msgs := h.rt.Bus().History("#general", 0)
	if len(msgs) != 1 || msgs[0].Text != "@scout please look" || msgs[0].Author != bus.Human {
		t.Fatalf("history = %+v", msgs)
	}
	if got := string(h.m.chat.input); got != "" {
		t.Errorf("input not cleared: %q", got)
	}
}

func TestChatTurnReplyAppearsInTranscript(t *testing.T) {
	h := newChatEditor(t)
	h.mock.Add(provider.ScriptText("All quiet on the western front."))
	trig, err := h.rt.Bus().Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout status"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatalf("turn: %v", err)
	}
	h.openFocused()
	view := plainView(h.m)
	if !strings.Contains(view, "scout") || !strings.Contains(view, "All quiet on the western front.") {
		t.Fatal("reply not rendered in sidebar")
	}
}

func TestChatApprovalFlow(t *testing.T) {
	h := newChatEditor(t)
	h.mock.Add(
		provider.ScriptToolCall("w1", "write", []byte(`{"path":"alpha/docs/n.md","content":"ok"}`)),
		provider.ScriptText("Done."),
	)
	trig, _ := h.rt.Bus().Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout document"})
	done := make(chan error, 1)
	go func() { done <- h.rt.Turn(context.Background(), "scout", trig) }()

	h.openFocused()
	deadline := time.After(2 * time.Second)
	for len(h.apprs.List()) == 0 {
		select {
		case <-h.apprs.Changes():
		case <-deadline:
			t.Fatal("approval never surfaced")
		}
	}
	if !strings.Contains(plainView(h.m), "approvals") {
		t.Fatal("approvals section not rendered")
	}
	h.m.HandleKey("y")
	if err := <-done; err != nil {
		t.Fatalf("turn errored: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(h.ws.Members[0].Path, "docs", "n.md"))
	if err != nil || string(data) != "ok" {
		t.Errorf("approved write missing: %q %v", data, err)
	}
}

func TestApplySuggestionIntoBuffer(t *testing.T) {
	h := newChatEditor(t)
	// Open a buffer first.
	feed(h.m, "/")
	typeKeys(h.m, "app.go")
	h.m.HandleKey("enter")

	if _, err := h.rt.Bus().Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "Try this:\n```go\nfmt.Println(\"applied\")\n```"}); err != nil {
		t.Fatal(err)
	}

	h.openFocused()
	before := h.m.active().Buffer().Text()
	if strings.Contains(before, "applied") {
		t.Fatal("buffer already contains suggestion")
	}
	h.m.HandleKey("ctrl+f")
	after := h.m.active().Buffer().Text()
	if !strings.Contains(after, `fmt.Println("applied")`) {
		t.Errorf("apply failed; buffer = %q", after)
	}
}
