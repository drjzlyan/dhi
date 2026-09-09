package runtime

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// echoStub is a fixture claude CLI: it base64-encodes each argv entry
// into $DUMPDIR/cli-args.dump (one per line, robust against embedded
// newlines), JSON-escapes the prompt text, and returns it as the final
// result. argv is ["-p", <prompt>, ...], so the prompt is $2.
const echoStub = `#!/bin/sh
: > "$DUMPDIR/cli-args.dump"
for a in "$@"; do
  printf '%s' "$a" | base64 >> "$DUMPDIR/cli-args.dump"
  printf '\n' >> "$DUMPDIR/cli-args.dump"
done
RES=$(printf '%s' "$2" | awk '{ if (NR>1) printf "%s", "\\n"; gsub(/"/, "\\\""); printf "%s", $0 }')
printf '%s\n' '{"type":"system","subtype":"init"}'
printf '%s\n' "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"$RES\",\"total_cost_usd\":0,\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}"
exit 0
`

// replyStub returns the fixed summary string as its final result.
const replyStub = `#!/bin/sh
echo '{"type":"system","subtype":"init"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"All quiet on the western front.","total_cost_usd":0}'
exit 0
`

// stubEnv provisions a binDir containing a claude CLI script (writing
// its arg dump into dumpDir, which must be a non-repo temp dir) and
// returns the reproducible CLIEnv for spawning it.
func stubEnv(t *testing.T, script string) (binDir, dumpDir string, cliEnv []string) {
	t.Helper()
	dir := t.TempDir()
	dump := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"PATH=" + dir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"DUMPDIR=" + dump,
	}
	return dir, dump, env
}

func stubRegistry(binDir string) *clirun.Registry {
	return clirun.NewRegistry(func(name string) (string, error) {
		p := filepath.Join(binDir, name)
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
		return p, nil
	})
}

// harness assembles a one-agent workspace, bus, approvals, and an
// echo-stubbed claude CLI for the roster agent.
type harness struct {
	ws        *workspace.Workspace
	bus       *bus.Bus
	approvals *tools.Approvals
	rt        *Runtime
	binDir    string
	dumpDir   string
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
	binDir, dumpDir, cliEnv := stubEnv(t, echoStub)

	m, err := manifest.Parse("scout", []byte(agentDoc))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	rt, err := New(Config{
		WS:        ws,
		Bus:       b,
		Approvals: ap,
		Sandbox:   sandbox.Noop{},
		CLIs:      stubRegistry(binDir),
		CLIEnv:    cliEnv,
	}, []*manifest.Agent{m})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{ws: ws, bus: b, approvals: ap, rt: rt, binDir: binDir, dumpDir: dumpDir}
}

func baseDoc() string {
	return `schema = 1
name = "Scout"
model = "m"
runtime = "claude"
system = "You scout."
tools = ["read", "write", "list"]
policy_json = """{"rules":[{"op":"read","path":"**","effect":"allow"}]}"""
`
}

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
		case <-time.After(5 * time.Second):
			t.Fatal("no reply within timeout")
			return bus.Message{}
		}
	}
}

func TestMentionTriggersTurnAndReply(t *testing.T) {
	h := newHarness(t, baseDoc())
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	trig, err := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout status?"})
	if err != nil {
		t.Fatal(err)
	}
	h.rt.Handle(context.Background(), trig)

	got := waitReply(t, replies)
	// The CLI gets the mention-stripped trigger as its prompt and echoes
	// it back as the final result, so the reply proves the strip.
	if got.Author != "scout" || got.Text != "status?" || got.Thread != 0 {
		t.Errorf("reply = %+v", got)
	}

	// The rendered system block must ground the CLI on the workspace
	// layout (arguments dumped by the echo stub).
	args := readArgs(t, h)
	var system, prompt string
	for i, a := range args {
		if a == "--append-system-prompt" && i+1 < len(args) {
			system = args[i+1]
		}
		if a == "-p" && i+1 < len(args) {
			prompt = args[i+1]
		}
	}
	if prompt != "status?" {
		t.Errorf("prompt = %q, want stripped trigger", prompt)
	}
	if !contains(system, "Members: api") {
		t.Errorf("system not grounded: %q", system)
	}
}

func TestDMTriggersWithoutMention(t *testing.T) {
	h := newHarness(t, baseDoc())
	replies, cancel := h.bus.Subscribe("dm:scout")
	defer cancel()

	h.rt.Handle(context.Background(), mustPost(t, h, bus.Message{Channel: "dm:scout", Author: bus.Human, Text: "hey"}))
	got := waitReply(t, replies)
	if got.Author != "scout" || got.Text != "hey" {
		t.Errorf("reply = %+v", got)
	}
}

func TestUnknownAgentIgnored(t *testing.T) {
	h := newHarness(t, baseDoc())
	mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@ghost hello"})
	// No configured agent is addressed; Handle must not post anything.
	h.rt.Handle(context.Background(), bus.Message{Channel: "#general", Author: bus.Human, Text: "@ghost hello"})
	time.Sleep(100 * time.Millisecond)
	if n := len(h.bus.History("#general", 0)); n != 1 {
		t.Fatalf("history = %d messages, want only the trigger", n)
	}
}

// mustAgent parses a minimal CLI-runtime manifest.
func mustAgent(t *testing.T, id, name string) *manifest.Agent {
	t.Helper()
	m, err := manifest.Parse(id, []byte("schema = 1\nname = \""+name+"\"\nmodel = \"m\"\nruntime = \"claude\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReloadSwapsRosterAndPings(t *testing.T) {
	h := newHarness(t, baseDoc())
	if err := h.rt.Reload([]*manifest.Agent{mustAgent(t, "bob", "Bob")}); err != nil {
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

	// A broken entry aborts and keeps the previous roster: a registry
	// whose binary lookup fails makes buildEntry refuse that agent.
	h.rt.agents = map[string]*entry{"scout": {m: mustAgent(t, "scout", "Scout")}}
	h.rt.cfg.CLIs = clirun.NewRegistry(func(string) (string, error) {
		return "", os.ErrNotExist
	})
	if err := h.rt.Reload([]*manifest.Agent{mustAgent(t, "bad", "Bad")}); err == nil {
		t.Fatal("reload with unresolvable CLI accepted")
	}
	if ids := h.rt.AgentIDs(); len(ids) != 1 || ids[0] != "scout" {
		t.Fatalf("previous roster not kept: %v", ids)
	}
}

func TestTurnsContinueAcrossReload(t *testing.T) {
	h := newHarness(t, baseDoc())
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	trig, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout one"})
	h.rt.Handle(context.Background(), trig)
	if got := waitReply(t, replies); got.Text != "one" {
		t.Fatalf("first reply = %+v", got)
	}

	if err := h.rt.Reload([]*manifest.Agent{mustAgent(t, "scout", "Scout")}); err != nil {
		t.Fatal(err)
	}
	trig2, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout two"})
	h.rt.Handle(context.Background(), trig2)
	got := waitReply(t, replies)
	// The stub echoes the full rendered prompt back, so the reply must
	// begin with the mention-stripped trigger and weave in history.
	if !strings.HasPrefix(got.Text, "two\n\nEarlier in this thread:\n- you (the human): @scout one") {
		t.Fatalf("post-reload reply = %+v", got)
	}
}

func TestStandardsInjectedIntoSystem(t *testing.T) {
	h := newHarness(t, baseDoc())
	if err := standards.Save(h.ws.Root, []string{"use conventional commits"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	rt, err := New(Config{
		WS:        h.ws,
		Bus:       h.bus,
		Approvals: h.approvals,
		Sandbox:   sandbox.Noop{},
		CLIs:      stubRegistry(h.binDir),
		CLIEnv: []string{"PATH=" + h.binDir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
			"DUMPDIR=" + h.dumpDir},
		Standards: true,
	}, []*manifest.Agent{mustAgent(t, "scout", "Scout")})
	if err != nil {
		t.Fatal(err)
	}
	replies, cancel := h.bus.Subscribe("#general")
	defer cancel()

	trig, _ := h.bus.Post(bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	rt.Handle(context.Background(), trig)
	waitReply(t, replies)

	args := readArgs(t, h)
	system := ""
	for i, a := range args {
		if a == "--append-system-prompt" && i+1 < len(args) {
			system = args[i+1]
			break
		}
	}
	for _, want := range []string{"Standing instructions", "conventional commits", "force-push"} {
		if !contains(system, want) {
			t.Errorf("system missing %q:\n%s", want, system)
		}
	}
}

// readArgs decodes the base64 argument dump the echo stub wrote into
// the harness's dump dir.
func readArgs(t *testing.T, h *harness) []string {
	t.Helper()
	path := filepath.Join(h.dumpDir, "cli-args.dump")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no argument dump at %s (stub did not run?): %v", path, err)
	}
	var args []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		dec, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			t.Fatalf("bad base64 arg line %q: %v", line, err)
		}
		args = append(args, string(dec))
	}
	return args
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

// recordingSandbox observes Wrap calls; it never rewrites argv.
type recordingSandbox struct{ wraps int }

func (r *recordingSandbox) Name() string { return "recording" }
func (r *recordingSandbox) Wrap(argv []string) ([]string, error) {
	r.wraps++
	return argv, nil
}

func TestSandboxInjectedIntoEveryGuard(t *testing.T) {
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
	rec := &recordingSandbox{}
	binDir, _, cliEnv := stubEnv(t, echoStub)
	doc := `schema = 1
name = "Scout"
model = "m"
runtime = "claude"
policy_json = """{"rules":[{"op":"exec","effect":"allow"}]}"""
`
	m, err := manifest.Parse("scout", []byte(doc))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	rt, err := New(Config{WS: ws, Bus: nil, Approvals: tools.NewApprovals(),
		Sandbox: rec, CLIs: stubRegistry(binDir), CLIEnv: cliEnv},
		[]*manifest.Agent{m})
	if err != nil {
		t.Fatal(err)
	}
	g, ok := rt.Guard("scout")
	if !ok {
		t.Fatal("no guard for scout")
	}
	if g.Sandbox.Name() != "recording" {
		t.Fatalf("guard sandbox = %q, want recording", g.Sandbox.Name())
	}
	// Guard.Exec routes through the injected adapter.
	if _, _, err := g.Exec([]string{"true"}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if rec.wraps != 1 {
		t.Fatalf("wraps = %d, want 1", rec.wraps)
	}
	// Unknown agent has no guard.
	if _, ok := rt.Guard("ghost"); ok {
		t.Fatal("ghost should have no guard")
	}
}

func TestNilSandboxRefused(t *testing.T) {
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
	// ADR-0011: production must inject an adapter explicitly; nil is a
	// silent Noop fallback and is rejected.
	if _, err := New(Config{WS: ws, Bus: nil},
		[]*manifest.Agent{mustAgent(t, "scout", "Scout")}); err == nil {
		t.Fatal("nil Sandbox must be refused")
	} else if !contains(err.Error(), "sandbox") {
		t.Errorf("error should name the seam: %v", err)
	}
}

func TestEmptyRosterRefused(t *testing.T) {
	if _, err := New(Config{}, nil); err == nil {
		t.Fatal("empty roster must be refused")
	}
}

func TestMalformedStandardsRefuseTurn(t *testing.T) {
	h := newHarness(t, baseDoc())
	if err := os.MkdirAll(filepath.Join(h.ws.Root, workspace.DHIDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.ws.Root, workspace.DHIDir, "standards.toml"),
		[]byte("not toml {{"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := New(Config{
		WS:        h.ws,
		Bus:       h.bus,
		Approvals: h.approvals,
		Sandbox:   sandbox.Noop{},
		CLIs:      stubRegistry(h.binDir),
		CLIEnv: []string{"PATH=" + h.binDir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
			"DUMPDIR=" + h.dumpDir},
		Standards: true,
	}, []*manifest.Agent{mustAgent(t, "scout", "Scout")})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.Turn(context.Background(), "scout", bus.Message{Channel: "#general"})
	if err == nil || !contains(err.Error(), "standards.toml") {
		t.Fatalf("turn must refuse with the path, got: %v", err)
	}
}
