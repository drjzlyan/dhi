package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const cliSuccessStub = `#!/bin/sh
echo '{"type":"system","subtype":"init"}'
echo "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"cwd: $PWD\"}]}}"
echo "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"secrets: ${ANTHROPIC_API_KEY:-unset} / leak: ${FOO:-unset}\"}]}}"
echo '{"type":"result","subtype":"success","is_error":false,"result":"Summary: build is green.","total_cost_usd":0.02,"usage":{"input_tokens":10,"output_tokens":5}}'
exit 0
`

const cliErrorStub = `#!/bin/sh
echo '{"type":"system","subtype":"init"}'
echo '{"type":"result","subtype":"error_max_turns","is_error":true,"result":""}'
exit 1
`

const cliHangStub = `#!/bin/sh
exec sleep 30
`

// cliHarness assembles a workspace with a task bound to a thread, a
// stub claude CLI on a fake PATH, and a CLI-runtime-rostered agent.
type cliHarness struct {
	ws  *workspace.Workspace
	b   *bus.Bus
	st  *tasks.Store
	rt  *Runtime
	rec *recordingSandbox
}

func newCLIHarness(t *testing.T, stubScript, agentDoc string) *cliHarness {
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
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &recordingSandbox{}
	reg := clirun.NewRegistry(func(name string) (string, error) {
		p := filepath.Join(binDir, name)
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
		return p, nil
	})

	m, err := manifest.Parse("scout", []byte(agentDoc))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	st, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Create("fix-login", "Fix login race", "you", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordChangeSet("fix-login", tasks.ChangeSet{Member: "you", Branch: "fix", Path: "wt"}); err != nil {
		t.Fatal(err)
	}
	if err := st.BindThread("fix-login", "#foo", 5); err != nil {
		t.Fatal(err)
	}

	rt, err := New(Config{
		WS:        ws,
		Bus:       b,
		Approvals: tools.NewApprovals(),
		Sandbox:   rec,
		CLIs:      reg,
		CLIEnv:    []string{"PATH=" + binDir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin"},
		Tasks:     st,
	}, []*manifest.Agent{m})
	if err != nil {
		t.Fatal(err)
	}
	return &cliHarness{ws: ws, b: b, st: st, rt: rt, rec: rec}
}

func (h *cliHarness) trigger() bus.Message {
	return bus.Message{Channel: "#foo", Thread: 5, Author: "you", Text: "@scout make the login safe"}
}

// drainUntil collects channel messages until an agent-authored message
// containing needle arrives (or a hard timeout).
// collectUntil drains messages from an existing (pre-turn) subscription
// until an agent-authored message containing needle arrives (or a hard
// timeout).
func collectUntil(t *testing.T, ch <-chan bus.Message, needle string) []bus.Message {
	t.Helper()
	var msgs []bus.Message
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m, ok := <-ch:
			if !ok {
				t.Fatal("channel closed")
			}
			msgs = append(msgs, m)
			if m.Author == "scout" && strings.Contains(m.Text, needle) {
				return msgs
			}
		case <-deadline:
			t.Fatal("timed out waiting for agent reply")
		}
	}
}

func TestCLIRunSuccess(t *testing.T) {
	h := newCLIHarness(t, cliSuccessStub, `schema = 1
name = "Scout"
model = "m"
runtime = "claude"
`)
	os.Setenv("ANTHROPIC_API_KEY", "sk-test-123")
	os.Setenv("FOO", "should-not-cross")
	t.Cleanup(func() { os.Unsetenv("ANTHROPIC_API_KEY"); os.Unsetenv("FOO") })

	ch, cancel := h.b.Subscribe("#foo")
	defer cancel()
	if err := h.rt.Turn(context.Background(), "scout", h.trigger()); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	msgs := collectUntil(t, ch, "Summary:")

	var cwd, secrets string
	for _, m := range msgs {
		if strings.Contains(m.Text, "cwd:") {
			cwd = m.Text
		}
		if strings.Contains(m.Text, "secrets:") {
			secrets = m.Text
		}
	}
	wantWd := filepath.Join(h.ws.Root, "wt")
	if real, err := filepath.EvalSymlinks(wantWd); err == nil {
		wantWd = real // macOS mounts /tmp and /var under /private
	}
	if cwd != "cwd: "+wantWd {
		t.Errorf("run cwd = %q, want worktree %q", cwd, wantWd)
	}
	if !strings.Contains(secrets, "sk-test-123") || !strings.Contains(secrets, "leak: unset") {
		t.Errorf("declared pass-through broke: %q", secrets)
	}
	if h.rec.wraps != 1 {
		t.Errorf("sandbox wraps = %d, want 1 (OS boundary is the wrap seam)", h.rec.wraps)
	}
	// Wrap contract (binary-first): the argv the sandbox receives must
	// start with the CLI itself, so the wrapped command is complete and
	// exec runs wrapped[0]. The regression here was real: passing the
	// adapter args alone made the sandbox wrapper's `--` separator feed
	// the sandbox invocation to claude AS THE PROMPT, and the reply
	// came back as plain text ("malformed final line").
	if len(h.rec.lastArg) == 0 || filepath.Base(h.rec.lastArg[0]) != "claude" {
		t.Errorf("Wrap argv[0] = %v, want the claude binary first", h.rec.lastArg)
	}
	if len(h.rec.lastArg) > 1 && h.rec.lastArg[1] != "-p" {
		t.Errorf("Wrap argv[1] = %q, want the adapter's first flag", h.rec.lastArg[1])
	}

	task, _ := h.st.Get("fix-login")
	if len(task.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(task.Runs))
	}
	run := task.Runs[0]
	if run.Status != tasks.RunOK || run.Summary != "Summary: build is green." {
		t.Errorf("run = %+v", run)
	}
	if run.TokensIn != 10 || run.TokensOut != 5 || run.CostUSD != 0.02 {
		t.Errorf("run usage = %+v", run)
	}
}

func TestCLIRunErrorRecordsRun(t *testing.T) {
	h := newCLIHarness(t, cliErrorStub, `schema = 1
name = "Scout"
model = "m"
runtime = "claude"
`)
	if err := h.rt.Turn(context.Background(), "scout", h.trigger()); err == nil {
		t.Fatal("Turn must report a failed run")
	} else if !strings.Contains(err.Error(), "error_max_turns") {
		t.Errorf("Turn error = %v", err)
	}

	task, _ := h.st.Get("fix-login")
	if len(task.Runs) != 1 || task.Runs[0].Status != tasks.RunError {
		t.Fatalf("runs = %+v, want one error run", task.Runs)
	}
	if !strings.Contains(task.Runs[0].Error, "error_max_turns") {
		t.Errorf("run error = %q", task.Runs[0].Error)
	}
}

func TestCLIRunTimeoutRecordsTimeout(t *testing.T) {
	h := newCLIHarness(t, cliHangStub, `schema = 1
name = "Scout"
model = "m"
runtime = "claude"
timeout = "1s"
`)
	if err := h.rt.Turn(context.Background(), "scout", h.trigger()); err == nil {
		t.Fatal("Turn must report a timed-out run")
	}

	task, _ := h.st.Get("fix-login")
	if len(task.Runs) != 1 || task.Runs[0].Status != tasks.RunTimeout {
		t.Fatalf("runs = %+v, want one timeout run", task.Runs)
	}
	if !strings.Contains(task.Runs[0].Error, "run interrupted") {
		t.Errorf("run error = %q", task.Runs[0].Error)
	}
}

func TestCLIRuntimeWithoutRegistryRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	ws, _ := workspace.Load(root)
	b, _ := bus.Open(ws)
	m, err := manifest.Parse("scout", []byte(`schema = 1
name = "Scout"
model = "m"
runtime = "claude"
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{WS: ws, Bus: b, Sandbox: sandbox.Noop{}}, []*manifest.Agent{m})
	if err == nil || !strings.Contains(err.Error(), "CLI registry") {
		t.Fatalf("expected CLI-registry refusal, got %v", err)
	}
}
