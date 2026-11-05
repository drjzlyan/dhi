package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const (
	// A claude that is signed out: it fails before any tool ran.
	foSignedOutStub = `#!/bin/sh
echo '{"type":"system","subtype":"init"}'
echo '{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}'
exit 1
`
	// A claude that ran a tool, then hit its rate limit.
	foAfterToolStub = `#!/bin/sh
echo '{"type":"system","subtype":"init"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","id":"t1","input":{"command":"touch made.txt"}}]}}'
echo '{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1760000000"}'
exit 1
`
	foCodexOKStub = `#!/bin/sh
echo "$@" > "$(dirname "$0")/args"
echo '{"type":"thread.started","thread_id":"t-1"}'
echo '{"type":"item.completed","item":{"type":"agent_message","id":"m1","text":"Done on the fallback."}}'
echo '{"type":"turn.completed","thread_id":"t-1","usage":{"input_tokens":4,"output_tokens":2}}'
exit 0
`
	foAgentDoc = `schema = 7
name = "Scout"
model = "claude-x"
engine = "cli:claude"
fallback_engine = "cli:codex"
fallback_model = "codex-x"
`
)

type foHarness struct {
	ws       *workspace.Workspace
	b        *bus.Bus
	rt       *Runtime
	ap       *tools.Approvals
	led      *budget.Ledger
	binDir   string
	mu       sync.Mutex
	failures []string // OnFailover calls, "agent from to why"
}

// newFailoverHarness roots a one-agent workspace whose PATH holds the given
// stub CLIs. A CLI absent from stubs is "not installed".
func newFailoverHarness(t *testing.T, doc string, stubs map[string]string) *foHarness {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
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
	binDir := t.TempDir()
	for name, script := range stubs {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m, err := manifest.Parse("scout", []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	st, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	h := &foHarness{ws: ws, b: b, ap: tools.NewApprovals(), led: budget.Open(root), binDir: binDir}
	rt, err := New(Config{
		WS: ws, Bus: b, Approvals: h.ap, Sandbox: sandbox.Noop{},
		CLIs:   stubRegistry(binDir),
		CLIEnv: []string{"PATH=" + binDir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin"},
		Tasks:  st, Spend: h.led,
		OnFailover: func(agent, from, to, why string) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.failures = append(h.failures, agent+" "+from+" "+to+" "+why)
		},
	}, []*manifest.Agent{m})
	if err != nil {
		t.Fatal(err)
	}
	h.rt = rt
	return h
}

func (h *foHarness) turn(t *testing.T) error {
	t.Helper()
	trig, _ := h.b.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	return h.rt.Turn(context.Background(), "scout", trig)
}

func (h *foHarness) transcripts(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	for _, p := range transcriptsUnder(t, h.ws.Root, "scout") {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sb.WriteString(filepath.Base(p) + "\n" + string(data))
	}
	return sb.String()
}

func TestFailoverSignedOutRunsOnFallback(t *testing.T) {
	h := newFailoverHarness(t, foAgentDoc, map[string]string{"claude": foSignedOutStub, "codex": foCodexOKStub})
	if err := h.turn(t); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	hist := h.b.History("#general", 0)
	if hasPrefix(hist, "switching engine: cli:claude → cli:codex (auth)") == nil {
		t.Fatalf("no failover notice: %+v", hist)
	}
	if has(hist, "Done on the fallback.") == nil {
		t.Fatalf("no reply from the fallback: %+v", hist)
	}
	h.mu.Lock()
	got := append([]string(nil), h.failures...)
	h.mu.Unlock()
	if len(got) != 1 || got[0] != "scout cli:claude cli:codex auth" {
		t.Fatalf("OnFailover = %v", got)
	}
	tr := h.transcripts(t)
	if !strings.Contains(tr, `"kind":"engine_failover"`) || !strings.Contains(tr, "cli:claude → cli:codex (auth)") {
		t.Fatalf("no engine_failover event in the transcripts:\n%s", tr)
	}
	args, _ := os.ReadFile(filepath.Join(h.binDir, "args"))
	if !strings.Contains(string(args), "codex-x") || strings.Contains(string(args), "claude-x") {
		t.Fatalf("fallback ran with %q; want its own model codex-x, not the primary's", args)
	}
	entries, err := h.led.Entries()
	if err != nil || len(entries) != 2 {
		t.Fatalf("ledger = %+v err=%v", entries, err)
	}
	if entries[0].Engine != "claude" || entries[1].Engine != "codex" {
		t.Fatalf("ledger engines = %q, %q; want the engine that actually ran", entries[0].Engine, entries[1].Engine)
	}
}

func TestFailoverNotAfterAToolRan(t *testing.T) {
	h := newFailoverHarness(t, foAgentDoc, map[string]string{"claude": foAfterToolStub, "codex": foCodexOKStub})
	if err := h.turn(t); err == nil {
		t.Fatal("a failure after a tool call must stay a failure")
	}
	hist := h.b.History("#general", 0)
	if hasPrefix(hist, "switching engine") != nil {
		t.Fatalf("failed over after side effects: %+v", hist)
	}
	if has(hist, "Done on the fallback.") != nil {
		t.Fatal("the fallback ran after a tool call")
	}
	h.mu.Lock()
	n := len(h.failures)
	h.mu.Unlock()
	if n != 0 {
		t.Fatalf("OnFailover fired %d times", n)
	}
}

func TestFailoverNeedsAFallbackAndAClosedClass(t *testing.T) {
	crash := `#!/bin/sh
echo '{"type":"system","subtype":"init"}'
echo '{"type":"result","subtype":"error","is_error":true,"result":"segfault in tool"}'
exit 1
`
	cases := []struct {
		name  string
		doc   string
		stubs map[string]string
	}{
		{"fallback none", strings.Replace(foAgentDoc, "fallback_engine = \"cli:codex\"\nfallback_model = \"codex-x\"\n", "fallback_engine = \"none\"\n", 1),
			map[string]string{"claude": foSignedOutStub, "codex": foCodexOKStub}},
		{"fallback never chosen", "schema = 7\nname = \"Scout\"\nmodel = \"m\"\nengine = \"cli:claude\"\n",
			map[string]string{"claude": foSignedOutStub, "codex": foCodexOKStub}},
		{"a crash is not a failover class", foAgentDoc, map[string]string{"claude": crash, "codex": foCodexOKStub}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newFailoverHarness(t, c.doc, c.stubs)
			if err := h.turn(t); err == nil {
				t.Fatal("turn must fail without a failover")
			}
			if hasPrefix(h.b.History("#general", 0), "switching engine") != nil {
				t.Fatal("failed over where the rules forbid it")
			}
		})
	}
}

// A fallback that is not installed is named, and the original failure
// stands (no silent substitution, no third engine).
func TestFailoverFallbackNotInstalled(t *testing.T) {
	h := newFailoverHarness(t, foAgentDoc, map[string]string{"claude": foSignedOutStub})
	if err := h.turn(t); err == nil {
		t.Fatal("turn must fail")
	}
	if hasPrefix(h.b.History("#general", 0), "not switching to the fallback engine cli:codex") == nil {
		t.Fatalf("missing refusal note: %+v", h.b.History("#general", 0))
	}
}

// The primary is not installed: the agent runs on the fallback from its
// first run, announced as a not_installed failover.
func TestFailoverPrimaryNotInstalled(t *testing.T) {
	h := newFailoverHarness(t, foAgentDoc, map[string]string{"codex": foCodexOKStub})
	if err := h.turn(t); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	hist := h.b.History("#general", 0)
	if hasPrefix(hist, "switching engine: cli:claude → cli:codex (not_installed)") == nil {
		t.Fatalf("no failover notice: %+v", hist)
	}
	if has(hist, "Done on the fallback.") == nil {
		t.Fatalf("fallback did not answer: %+v", hist)
	}
	if !strings.Contains(h.transcripts(t), `"kind":"engine_failover"`) {
		t.Fatal("no engine_failover transcript event")
	}
}

func TestPrimaryNotInstalledWithoutFallbackStillRefuses(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, _ := workspace.Load(root)
	b, _ := bus.Open(ws)
	m, err := manifest.Parse("scout", []byte("schema = 7\nname = \"S\"\nmodel = \"m\"\nengine = \"cli:claude\"\nfallback_engine = \"none\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{WS: ws, Bus: b, Sandbox: sandbox.Noop{}, CLIs: stubRegistry(t.TempDir())}, []*manifest.Agent{m})
	if err == nil || !strings.Contains(err.Error(), "install claude") {
		t.Fatalf("err = %v", err)
	}
}

// A fallback whose native-web or MCP wiring differs from the primary's
// asks before its first use; a denial leaves the failure as it was.
func TestFailoverWiringDifferenceAsksFirst(t *testing.T) {
	doc := strings.NewReplacer("cli:codex", "cli:cursor-agent", "fallback_model = \"codex-x\"\n", "").Replace(foAgentDoc)
	h := newFailoverHarness(t, doc, map[string]string{"claude": foSignedOutStub, "cursor-agent": foCodexOKStub})
	done := make(chan error, 1)
	go func() { done <- h.turn(t) }()
	deadline := time.After(asyncTimeout)
	for {
		list := h.ap.List()
		if len(list) > 0 {
			ap := list[0]
			if !strings.Contains(ap.Reason, "differs from claude") {
				t.Fatalf("card reason = %q", ap.Reason)
			}
			h.ap.Resolve(ap.ID, false)
			break
		}
		select {
		case <-deadline:
			t.Fatal("no approval card for a fallback with different wiring")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := <-done; err == nil {
		t.Fatal("a denied failover must leave the turn failed")
	}
	if hasPrefix(h.b.History("#general", 0), "not switching to the fallback engine cli:cursor-agent") == nil {
		t.Fatalf("denial not named: %+v", h.b.History("#general", 0))
	}
}

func TestFallbackStatus(t *testing.T) {
	reg := stubRegistry(t.TempDir())
	cases := []struct {
		name     string
		m        manifest.Agent
		resolves bool
		why      string
	}{
		{"unset", manifest.Agent{ID: "a"}, false, "no fallback chosen"},
		{"none", manifest.Agent{ID: "a", FallbackEngine: "none"}, false, "none, by choice"},
		{"missing", manifest.Agent{ID: "a", FallbackEngine: "cli:codex", FallbackRuntime: "codex"}, false, "codex is not installed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := manifest.FallbackFor(reg, &c.m, "claude")
			if st.Resolves != c.resolves || !strings.Contains(st.Why, c.why) {
				t.Fatalf("got %+v", st)
			}
		})
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	st := manifest.FallbackFor(stubRegistry(dir), &manifest.Agent{ID: "a", FallbackEngine: "cli:codex", FallbackRuntime: "codex"}, "claude")
	if !st.Resolves || st.Fallback != "cli:codex" || st.Engine != "cli:claude" {
		t.Fatalf("resolving fallback = %+v", st)
	}
}
