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
	"github.com/drjzlyan/dhi/internal/agentkit/memory"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// F-065: a threaded trigger replays its own thread (root + replies), the
// agent's real replies as "you (earlier)", and never relayed stream
// lines or the channel's unrelated top level.
func TestPromptThreadScopedHistory(t *testing.T) {
	h := newHarness(t, baseDoc())
	mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "unrelated chatter"})
	root := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout fix the login bug"})
	mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: "scout", Text: "progress: reading auth.go", Kind: bus.KindStream})
	mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: "scout", Text: "legacy unmarked progress line"})
	mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: "scout", Text: "Found it: the token check is inverted.", Kind: bus.KindReply})
	trig := mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: bus.Human, Text: "@scout great, fix it"})

	prompt, _ := h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], trig, "")
	for _, want := range []string{
		"you (the human): @scout fix the login bug",
		"you (earlier): Found it: the token check is inverted.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, bad := range []string{"unrelated chatter", "progress: reading auth.go", "legacy unmarked progress line"} {
		if strings.Contains(prompt, bad) {
			t.Fatalf("prompt carries %q:\n%s", bad, prompt)
		}
	}
	if strings.Index(prompt, "fix the login bug") > strings.Index(prompt, "Found it") {
		t.Fatalf("thread root must come first:\n%s", prompt)
	}
}

func TestPromptHistoryNamesOmissions(t *testing.T) {
	h := newHarness(t, baseDoc())
	for i := 0; i < 20; i++ {
		mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "note"})
	}
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	prompt, _ := h.rt.cliPromptAt(context.Background(), h.rt.agents["scout"], trig, "", maxCompaction, nil, false)
	if !strings.Contains(prompt, "(8 earlier messages omitted)") {
		t.Fatalf("compacted history must name what it dropped:\n%s", prompt)
	}
}

func TestPromptCarriesTaskCard(t *testing.T) {
	h := newHarness(t, baseDoc())
	st, err := tasks.Open(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	h.rt.cfg.Tasks = st
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout continue"})
	if err := st.Create("fix-login", "Fix the login redirect", "scout", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.BindThread("fix-login", "#general", trig.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddComment("fix-login", bus.Human, "keep the old cookie name"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordRun("fix-login", tasks.Run{ID: "run-1", Agent: "scout", Status: tasks.RunError,
		Error: "exit 2", Started: time.Now(), Finished: time.Now()}); err != nil {
		t.Fatal(err)
	}
	prompt, _ := h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], trig, "")
	for _, want := range []string{
		"Task card fix-login: Fix the login redirect",
		"keep the old cookie name",
		"error by scout: exit 2",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// The system block runs stable → volatile so the CLI's prompt cache
// keeps its prefix across turns.
func TestSystemBlockStableBeforeVolatile(t *testing.T) {
	h := newHarness(t, baseDoc())
	h.rt.cfg.Standards = true
	if err := standards.Save(h.ws.Root, []string{"use conventional commits"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	h.rt.cfg.Memory = memory.Open(h.ws)
	if err := h.rt.cfg.Memory.Append("scout", "lesson", "run gofmt"); err != nil {
		t.Fatal(err)
	}
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	_, system := h.rt.cliPrompt(context.Background(), h.rt.agents["scout"], trig, "WORKFLOW-BLOCK")
	order := []string{"use conventional commits", "WORKFLOW-BLOCK", "Your memory (persistent across turns)"}
	last := -1
	for _, s := range order {
		i := strings.Index(system, s)
		if i < 0 || i < last {
			t.Fatalf("system block order broken at %q:\n%s", s, system)
		}
		last = i
	}
}

// authStub fails like a logged-out claude.
const authStub = `#!/bin/sh
N=0
if [ -f "$DUMPDIR/attempts" ]; then N=$(cat "$DUMPDIR/attempts"); fi
printf '%s\n' "$((N+1))" > "$DUMPDIR/attempts"
echo 'Invalid API key · Please run /login' >&2
echo '{"type":"result","subtype":"error","is_error":true,"result":"Invalid API key · Please run /login"}'
exit 1
`

func TestRetryAuthFailsFastWithNotice(t *testing.T) {
	h := newHarnessScript(t, baseDoc()+"retries = 3\n", authStub)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	if err := h.rt.Turn(context.Background(), "scout", trig); err == nil {
		t.Fatal("auth failure must fail the turn")
	}
	if got := readIntFile(t, filepath.Join(h.dumpDir, "attempts")); got != 1 {
		t.Fatalf("spawns = %d, want 1 (auth never retries)", got)
	}
	hist := h.bus.History("#general", 0)
	n := hasPrefix(hist, "I couldn't finish: claude is not authenticated")
	if n == nil || n.Kind != bus.KindAlert {
		t.Fatalf("missing auth alert (F-072): %+v", hist)
	}
}

// crashThenEchoStub fails once with a stream error, then echoes its
// prompt so the test can read what the retry was told.
const crashThenEchoStub = `#!/bin/sh
N=0
if [ -f "$DUMPDIR/attempts" ]; then N=$(cat "$DUMPDIR/attempts"); fi
N=$((N+1))
printf '%s\n' "$N" > "$DUMPDIR/attempts"
if [ "$N" -lt 2 ]; then
  echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./auth"}}]}}'
  exit 2
fi
printf '%s' "$2" > "$DUMPDIR/prompt"
echo '{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{"input_tokens":1,"output_tokens":1}}'
`

func TestRetryCarriesFailureBrief(t *testing.T) {
	prev := cliRetryBackoff
	cliRetryBackoff = time.Millisecond
	defer func() { cliRetryBackoff = prev }()

	h := newHarnessScript(t, baseDoc()+"retries = 1\n", crashThenEchoStub)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatalf("turn: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(h.dumpDir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"Your previous attempt (1) failed [crash]", "go test ./auth", "do not redo finished work"} {
		if !strings.Contains(got, want) {
			t.Fatalf("retry prompt missing %q:\n%s", want, got)
		}
	}
	notice := hasPrefix(h.bus.History("#general", 0), "retrying (attempt 1/1)")
	if notice == nil || notice.Kind != bus.KindStream {
		t.Fatal("retry notice must be stream-marked")
	}
}

// overflowStub overflows on the first attempt and records the second
// attempt's system block.
const overflowStub = `#!/bin/sh
N=0
if [ -f "$DUMPDIR/attempts" ]; then N=$(cat "$DUMPDIR/attempts"); fi
N=$((N+1))
printf '%s\n' "$N" > "$DUMPDIR/attempts"
if [ "$N" -lt 2 ]; then
  echo '{"type":"result","subtype":"error","is_error":true,"result":"Prompt is too long"}'
  exit 1
fi
for a in "$@"; do printf '%s\n' "$a"; done > "$DUMPDIR/args"
echo '{"type":"result","subtype":"success","is_error":false,"result":"ok","usage":{"input_tokens":1,"output_tokens":1}}'
`

func TestRetryOverflowCompacts(t *testing.T) {
	h := newHarnessScript(t, baseDoc()+"retries = 2\n", overflowStub)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	start := time.Now()
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("an overflow retry must not wait the backoff")
	}
	b, err := os.ReadFile(filepath.Join(h.dumpDir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Context was compacted") {
		t.Fatalf("second attempt not compacted:\n%s", b)
	}
}

func TestRetryWaitByClass(t *testing.T) {
	prev := cliRetryBackoff
	cliRetryBackoff = time.Second
	defer func() { cliRetryBackoff = prev }()
	if d := retryWait(clirun.FailOverflow, 0); d != 0 {
		t.Fatalf("overflow wait = %s", d)
	}
	if d := retryWait(clirun.FailCrash, 3); d != time.Second {
		t.Fatalf("crash wait = %s", d)
	}
	a, b := retryWait(clirun.FailRateLimit, 0), retryWait(clirun.FailRateLimit, 2)
	if a < 2*time.Second || b < 8*time.Second || b <= a {
		t.Fatalf("rate-limit backoff must grow: %s then %s", a, b)
	}
}

// newHarnessScript is newHarness with a different claude stub script.
func newHarnessScript(t *testing.T, agentDoc, script string) *harness {
	t.Helper()
	h := newHarness(t, agentDoc)
	if err := os.WriteFile(filepath.Join(h.binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTailBufferKeepsLastLine(t *testing.T) {
	tb := &tailBuffer{max: 16}
	_, _ = tb.Write([]byte("warming up a long preamble\n"))
	_, _ = tb.Write([]byte("not logged in\n\n"))
	if got := tb.lastLine(); got != "not logged in" {
		t.Fatalf("lastLine = %q", got)
	}
}

// toolAuthStub: the agent's own `git push` failed with an auth error in
// the stream, then the CLI died. That is a crash to retry, never a CLI
// login problem.
const toolAuthStub = `#!/bin/sh
N=0
if [ -f "$DUMPDIR/attempts" ]; then N=$(cat "$DUMPDIR/attempts"); fi
N=$((N+1))
printf '%s\n' "$N" > "$DUMPDIR/attempts"
if [ "$N" -lt 2 ]; then
  echo '{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":"fatal: Authentication failed for https://github.com/x/y"}]}}'
  exit 2
fi
echo '{"type":"result","subtype":"success","is_error":false,"result":"pushed","usage":{"input_tokens":1,"output_tokens":1}}'
`

func TestRetryIgnoresToolAuthFailure(t *testing.T) {
	prev := cliRetryBackoff
	cliRetryBackoff = time.Millisecond
	defer func() { cliRetryBackoff = prev }()

	h := newHarnessScript(t, baseDoc()+"retries = 1\n", toolAuthStub)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout push"})
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if got := readIntFile(t, filepath.Join(h.dumpDir, "attempts")); got != 2 {
		t.Fatalf("spawns = %d, want 2 (a tool's auth error is a crash to retry)", got)
	}
	if hasPrefix(h.bus.History("#general", 0), "claude is not authenticated") != nil {
		t.Fatal("a tool's auth error must not claim the CLI is logged out")
	}
}

// The agent's final reply is marked so later prompts replay it.
func TestFinalReplyMarked(t *testing.T) {
	h := newHarness(t, baseDoc())
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout hi"})
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if m := has(h.bus.History("#general", 0), "hi"); m == nil || m.Kind != bus.KindReply {
		t.Fatalf("final reply not marked: %+v", m)
	}
}
