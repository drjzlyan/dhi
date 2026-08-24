package runtime

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
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// harness assembles a one-agent workspace, bus, and mock-backed runtime.
type harness struct {
	ws        *workspace.Workspace
	bus       *bus.Bus
	approvals *tools.Approvals
	mock      *provider.Mock
	rt        *Runtime
	signals   chan *tools.Approval
}

func newHarness(t *testing.T, agentDoc string) *harness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "api", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
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
	ap := tools.NewApprovals()
	signals := make(chan *tools.Approval, 8)
	ap.OnRequest = func(a *tools.Approval) { signals <- a }

	m, err := manifest.Parse("scout", []byte(agentDoc))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	h := &harness{
		ws:        ws,
		bus:       b,
		approvals: ap,
		mock:      provider.NewMock(),
		signals:   signals,
	}
	rt, err := New(Config{
		WS:        ws,
		Bus:       b,
		Approvals: ap,
		Provider:  h.mock,
	}, []*manifest.Agent{m})
	if err != nil {
		t.Fatal(err)
	}
	h.rt = rt
	return h
}

const baseDoc = `schema = 1
name = "Scout"
model = "mock-1"
system = "You scout."
tools = ["read", "write", "list"]
policy_json = """{"rules":[{"op":"read","path":"**","effect":"allow"},{"op":"write","path":"docs/**","effect":"ask"}]}"""
`

// waitReply drains until an agent-authored message arrives (subscribers
// also see the human's own trigger).
func waitReply(t *testing.T, ch <-chan bus.Message) bus.Message {
	t.Helper()
	for {
		select {
		case m := <-ch:
			if m.Author != bus.Human {
				return m
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no reply within timeout")
			return bus.Message{}
		}
	}
}

func TestMentionTriggersTurnAndReply(t *testing.T) {
	h := newHarness(t, baseDoc)
	h.mock.Add(provider.ScriptText("On it."))
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	trig, err := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout status?"})
	if err != nil {
		t.Fatal(err)
	}
	h.rt.Handle(context.Background(), trig)

	got := waitReply(t, replies)
	if got.Author != "scout" || got.Text != "On it." || got.Thread != 0 {
		t.Errorf("reply = %+v", got)
	}
	calls := h.mock.Calls()
	if len(calls) != 1 || calls[0].Model != "mock-1" {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].System == "" || !contains(calls[0].System, "Members: api") {
		t.Errorf("system prompt not grounded: %q", calls[0].System)
	}
	if len(calls[0].Tools) == 0 {
		t.Error("no tool defs sent")
	}
	last := calls[0].Messages[len(calls[0].Messages)-1]
	if last.Blocks[0].(provider.Text).Value != "status?" {
		t.Errorf("mention not stripped from trigger: %#v", last.Blocks[0])
	}
}

func TestToolRoundTripFeedsResultsBack(t *testing.T) {
	h := newHarness(t, baseDoc)
	h.mock.Add(
		provider.ScriptToolCall("t1", "read", []byte(`{"path":"api/main.go"}`)),
		provider.ScriptText("It says package main."),
	)
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	trig, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout read main"})
	h.rt.Turn(context.Background(), "scout", trig)

	got := waitReply(t, replies)
	if got.Text != "It says package main." {
		t.Errorf("reply = %q", got.Text)
	}
	rec := h.mock.Calls()[1]
	if len(rec.Messages) < 3 {
		t.Fatalf("second request has %d messages", len(rec.Messages))
	}
	res, ok := rec.Messages[len(rec.Messages)-1].Blocks[0].(provider.ToolResult)
	if !ok || res.Content != "package main\n" || res.IsError {
		t.Errorf("tool result block wrong: %#v", res)
	}
}

func TestDeniedWriteReportsErrorToModel(t *testing.T) {
	// Manifest policy allows reads only: writes default-deny.
	doc := `schema = 1
name = "Scout"
model = "mock-1"
system = "You scout."
tools = ["read", "write"]
policy_json = """{"rules":[{"op":"read","path":"**","effect":"allow"}]}"""
`
	h := newHarness(t, doc)
	h.mock.Add(
		provider.ScriptToolCall("w1", "write", []byte(`{"path":"api/x.txt","content":"no"}`)),
		provider.ScriptText("Could not write."),
	)
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	trig, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout write it"})
	h.rt.Turn(context.Background(), "scout", trig)

	waitReply(t, replies)
	res := h.mock.Calls()[1].Messages[len(h.mock.Calls()[1].Messages)-1].Blocks[0].(provider.ToolResult)
	if !res.IsError || !contains(res.Content, "denied") {
		t.Errorf("deny not surfaced to model: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(h.ws.Members()[0].Path, "x.txt")); !os.IsNotExist(err) {
		t.Error("denied write touched disk")
	}
}

func TestAskWriteRequiresApproval(t *testing.T) {
	doc := `schema = 1
name = "Scout"
model = "mock-1"
system = "You scout."
tools = ["read", "write"]
policy_json = """{"rules":[{"op":"read","path":"**","effect":"allow"},{"op":"write","path":"docs/**","effect":"ask"}]}"""
`
	h := newHarness(t, doc)
	h.mock.Add(
		provider.ScriptToolCall("w1", "write", []byte(`{"path":"api/docs/n.md","content":"ok"}`)),
		provider.ScriptText("Wrote with permission."),
	)
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	trig, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout document this"})
	done := make(chan error, 1)
	go func() { done <- h.rt.Turn(context.Background(), "scout", trig) }()

	select {
	case a := <-h.signals:
		h.approvals.Resolve(a.ID, true)
	case <-time.After(3 * time.Second):
		t.Fatal("approval never surfaced")
	}
	if err := <-done; err != nil {
		t.Fatalf("turn: %v", err)
	}
	waitReply(t, replies)
	data, err := os.ReadFile(filepath.Join(h.ws.Members()[0].Path, "docs", "n.md"))
	if err != nil || string(data) != "ok" {
		t.Errorf("approved write missing: %q %v", data, err)
	}
}

func TestDMTriggersWithoutMention(t *testing.T) {
	h := newHarness(t, baseDoc)
	h.mock.Add(provider.ScriptText("dm reply"))
	replies, cancel := h.bus.Subscribe("dm:scout")
	defer cancel()

	h.rt.Handle(context.Background(), mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "hey"}))
	got := waitReply(t, replies)
	if got.Author != "scout" || got.Text != "dm reply" {
		t.Errorf("reply = %+v", got)
	}
}

func TestUnknownAgentIgnored(t *testing.T) {
	h := newHarness(t, baseDoc)
	msg := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@ghost hello"})
	h.rt.Handle(context.Background(), msg)
	select {
	case c := <-callChan(h):
		if len(c) != 0 {
			t.Fatalf("unexpected provider call: %+v", c)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func callChan(h *harness) <-chan []provider.Request {
	ch := make(chan []provider.Request, 1)
	go func() { ch <- h.mock.Calls() }()
	return ch
}

func mustPost(t *testing.T, h *harness, m bus.Message) bus.Message {
	t.Helper()
	got, err := h.bus.Post(m)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return got
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// mustAgent parses a minimal manifest with the given id/name/tools.
func mustAgent(t *testing.T, id, name string, tools ...string) *manifest.Agent {
	t.Helper()
	doc := "schema = 1\nname = \"" + name + "\"\nmodel = \"mock-1\"\ntools = ["
	for i, tl := range tools {
		if i > 0 {
			doc += ", "
		}
		doc += "\"" + tl + "\""
	}
	doc += "]\n"
	m, err := manifest.Parse(id, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReloadSwapsRosterAndPings(t *testing.T) {
	h := newHarness(t, baseDoc)
	if err := h.rt.Reload([]*manifest.Agent{mustAgent(t, "bob", "Bob", "read")}); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	ids := h.rt.AgentIDs()
	if len(ids) != 1 || ids[0] != "bob" {
		t.Fatalf("ids after reload = %v", ids)
	}
	select {
	case <-h.rt.Changes():
	default:
		t.Fatal("no reload ping")
	}

	// Empty roster clears the crew without error.
	if err := h.rt.Reload(nil); err != nil {
		t.Fatalf("Reload(nil): %v", err)
	}
	if ids = h.rt.AgentIDs(); len(ids) != 0 {
		t.Fatalf("ids after clear = %v", ids)
	}

	// A broken entry aborts and keeps the previous roster: an explicit
	// nil provider override makes buildEntry fail for that agent.
	h.rt.agents = map[string]*entry{"scout": {m: mustAgent(t, "scout", "Scout")}}
	h.rt.cfg.Providers = map[string]provider.Provider{"bad": nil}
	if err := h.rt.Reload([]*manifest.Agent{mustAgent(t, "bad", "Bad")}); err == nil {
		t.Fatal("reload without provider accepted")
	}
	if ids := h.rt.AgentIDs(); len(ids) != 1 || ids[0] != "scout" {
		t.Fatalf("previous roster not kept: %v", ids)
	}
}

func TestTurnsContinueAcrossReload(t *testing.T) {
	h := newHarness(t, baseDoc)
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	h.mock.Add(provider.ScriptText("first"))
	trig, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout one"})
	h.rt.Handle(context.Background(), trig)
	waitReply(t, replies)

	fresh := mustAgent(t, "scout", "Scout", "read", "write", "list")
	h.mock.Add(provider.ScriptText("second"))
	if err := h.rt.Reload([]*manifest.Agent{fresh}); err != nil {
		t.Fatal(err)
	}
	trig2, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout two"})
	h.rt.Handle(context.Background(), trig2)
	got := waitReply(t, replies)
	if got.Text != "second" {
		t.Fatalf("post-reload reply = %+v", got)
	}
}

func TestStandardsInjectedIntoPrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "api", "main.go"), []byte("package main\n"), 0o644)
	workspace.Create(root, "api")
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	stdDoc := "schema = 1\nworkspace = [\"use conventional commits\"]\n"
	if err := standards.Save(root, []string{"use conventional commits"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = stdDoc

	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	mock := provider.NewMock()
	mock.Add(provider.ScriptText("ok"))
	rt, err := New(Config{WS: ws, Bus: b, Approvals: tools.NewApprovals(),
		Provider: mock, Standards: true}, []*manifest.Agent{mustAgent(t, "scout", "Scout")})
	if err != nil {
		t.Fatal(err)
	}
	replies, cancel := b.Subscribe("#general")
	defer cancel()
	trig, _ := b.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	rt.Handle(context.Background(), trig)
	waitReply(t, replies)

	calls := mock.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	sys := calls[0].System
	for _, want := range []string{"Standing instructions", "conventional commits", "force-push"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system missing %q:\n%s", want, sys)
		}
	}
}
