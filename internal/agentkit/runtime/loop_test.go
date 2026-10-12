package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/dhitools"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// fakeRunner scripts the verify command's results, one per call.
type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	dirs  []string
	outs  []string // "" = pass; else the failing output
}

func (f *fakeRunner) Run(_ context.Context, dir string, argv []string, _ bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, argv)
	f.dirs = append(f.dirs, dir)
	i := min(len(f.calls)-1, len(f.outs)-1)
	if f.outs[i] == "" {
		return "ok\n", nil
	}
	return f.outs[i], errors.New("exit status 1")
}

// loopHarness: a task card bound to the trigger with its own worktree
// (a Go module), episodes, the session stub and the scripted runner.
func loopHarness(t *testing.T, outs ...string) (*harness, *fakeRunner, *tasks.Store, bus.Message) {
	t.Helper()
	h, fr, st, trig := bareLoopHarness(t, outs...)
	if err := st.Attach("fix-bug", "api", "task/fix-bug", ""); err != nil {
		t.Fatal(err)
	}
	return h, fr, st, trig
}

// worktreeOf is the loop harness's task worktree.
func worktreeOf(h *harness) string {
	return filepath.Join(h.ws.Root, ".dhi", "worktrees", "fix-bug", "api")
}

// bareLoopHarness is loopHarness before any worktree is attached; its
// attach seam creates the worktree (with a go.mod) when called.
func bareLoopHarness(t *testing.T, outs ...string) (*harness, *fakeRunner, *tasks.Store, bus.Message) {
	t.Helper()
	h := episodeHarness(t)
	st, err := tasks.Open(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	st.SetAttach(func(slug, member, branch, _ string) (string, error) {
		wt := worktreeOf(h)
		if err := os.MkdirAll(wt, 0o755); err != nil {
			return "", err
		}
		return ".dhi/worktrees/fix-bug/api", os.WriteFile(filepath.Join(wt, "go.mod"), []byte("module x\n"), 0o644)
	}, nil)
	h.rt.cfg.Tasks = st
	fr := &fakeRunner{outs: outs}
	h.rt.cfg.Run = fr
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout fix the bug"})
	if err := st.Create("fix-bug", "Fix the bug", "scout", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.BindThread("fix-bug", "#general", trig.ID); err != nil {
		t.Fatal(err)
	}
	return h, fr, st, trig
}

func TestLoopVerifyPass(t *testing.T) {
	h, fr, st, trig := loopHarness(t, "")
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 1 || strings.Join(fr.calls[0], " ") != "go test ./..." || fr.dirs[0] != worktreeOf(h) {
		t.Fatalf("verify calls = %v in %v", fr.calls, fr.dirs)
	}
	if tk, _ := st.Get("fix-bug"); !tk.TestsPass {
		t.Fatal("a passing verify must set TestsPass")
	}
	if hasPrefix(h.bus.History("#general", 0), "verified: go test ./... passed (round 1)") == nil {
		t.Fatal("missing verified notice")
	}
	if has(h.bus.History("#general", 0), "reply 1") == nil {
		t.Fatal("the reply must still be posted")
	}
	ep, _, _ := h.rt.cfg.Episodes.Get("scout", episode.Key("fix-bug", "", 0))
	if ep.Verify != "pass" || ep.Iterations != 1 {
		t.Fatalf("episode verify = %+v", ep)
	}
}

func TestLoopVerifyFailThenFix(t *testing.T) {
	h, fr, st, trig := loopHarness(t, "--- FAIL: TestSplit (0.00s)\n    want 334 got 333", "")
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("verify rounds = %d, want 2", len(fr.calls))
	}
	second := spawnArgs(t, h, 2)
	if !strings.Contains(second, "--resume\nS1\n") || !strings.Contains(second, "want 334 got 333") ||
		!strings.Contains(second, "round 1 of 3") {
		t.Fatalf("the fix round must resume with the failure:\n%s", second)
	}
	if tk, _ := st.Get("fix-bug"); !tk.TestsPass {
		t.Fatal("TestsPass must end true after the fix")
	}
}

func TestLoopStuckAsksHuman(t *testing.T) {
	same := "--- FAIL: TestSplit (0.01s)\n    want 334 got 333"
	h, fr, st, trig := loopHarness(t, same, strings.Replace(same, "0.01s", "0.02s", 1))
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("stuck must stop after the repeat: %d rounds", len(fr.calls))
	}
	note := hasPrefix(h.bus.History("#general", 0), "@you I'm stuck")
	if note == nil || note.Kind != bus.KindReply {
		t.Fatal("stuck must ask the human (an @you reply)")
	}
	if tk, _ := st.Get("fix-bug"); tk.TestsPass {
		t.Fatal("TestsPass must be false while stuck")
	}
}

func TestLoopIterationCap(t *testing.T) {
	h, fr, _, trig := loopHarness(t, "fail A", "fail B", "fail C", "")
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Resume: true, MaxIterations: 2} }
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("rounds = %d, want the cap of 2", len(fr.calls))
	}
	if hasPrefix(h.bus.History("#general", 0), "@you `go test ./...` still fails after 2 rounds") == nil {
		t.Fatal("cap must hand back to the human")
	}
}

func TestChatTurnNeverVerifies(t *testing.T) {
	h := episodeHarness(t)
	fr := &fakeRunner{outs: []string{""}}
	h.rt.cfg.Run = fr
	st, _ := tasks.Open(h.ws)
	h.rt.cfg.Tasks = st
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout what is this?"})
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 0 {
		t.Fatal("a chat turn must not verify")
	}
}

// F-067: a task turn without a worktree gets one first, works in it, and
// is told so.
func TestProvisionWorktree(t *testing.T) {
	h, _, st, trig := bareLoopHarness(t, "")
	h.rt.cfg.Provision = func(slug, member string) error { return st.Attach(slug, member, "task/"+slug, "") }
	h.rt.cfg.Workflows = true
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if hasPrefix(h.bus.History("#general", 0), "created worktree .dhi/worktrees/fix-bug/api on branch task/fix-bug") == nil {
		t.Fatalf("missing provision notice: %+v", h.bus.History("#general", 0))
	}
	args := spawnArgs(t, h, 1)
	for _, want := range []string{
		"worktree: .dhi/worktrees/fix-bug/api on branch task/fix-bug",
		`member "api"'s checkout`, "[done] Work in a worktree", "DHI verifies with `go test ./...`",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("spawn missing %q:\n%s", want, args)
		}
	}
}

// Without a worktree, the workflow forbids making a private one (the P2
// eval failure: work DHI never saw).
func TestNoWorktreeForbidsPrivateOne(t *testing.T) {
	h, fr, _, trig := bareLoopHarness(t, "")
	h.rt.cfg.Workflows = true
	_ = h.rt.Turn(context.Background(), "scout", trig)
	if !strings.Contains(spawnArgs(t, h, 1), "Do NOT create one yourself") {
		t.Fatal("no-worktree guidance missing")
	}
	if len(fr.calls) != 0 {
		t.Fatal("no worktree, no verify (the shared checkout is not the task's to judge)")
	}
}

// Autopilot run cards are chores: never provisioned.
func TestAutopilotCardNotProvisioned(t *testing.T) {
	if !autopilotCard("auto-daily-12") || autopilotCard("fix-auth") {
		t.Fatal("autopilot card detection")
	}
}

func TestProvisionAmbiguousIsNamed(t *testing.T) {
	h, _, st, trig := bareLoopHarness(t, "")
	if err := os.MkdirAll(filepath.Join(h.ws.Root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.ws.AddMember("web", filepath.Join(h.ws.Root, "web")); err != nil {
		t.Fatal(err)
	}
	called := false
	h.rt.cfg.Provision = func(string, string) error { called = true; return nil }
	_ = h.rt.Turn(context.Background(), "scout", trig)
	_ = st
	if called || hasPrefix(h.bus.History("#general", 0), "task fix-bug has no worktree and the workspace has 2 members") == nil {
		t.Fatalf("ambiguous provision must be named, not guessed (called=%v)", called)
	}
}

// silentStub never prints; the watchdog must kill it.
const silentHangStub = `#!/bin/sh
sleep 5
`

func TestWatchdogKillsStall(t *testing.T) {
	h := newHarnessScript(t, baseDoc(), silentHangStub)
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Stall: 300 * time.Millisecond, MaxIterations: 1} }
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	start := time.Now()
	err := h.rt.Turn(context.Background(), "scout", trig)
	if err == nil || !strings.Contains(err.Error(), "stalled: no output for 300ms") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("the watchdog did not cut the spawn short")
	}
}

// A verify that cannot run here (no network for modules) is named, not
// sent back as a bug.
func TestLoopEnvFailureIsNamed(t *testing.T) {
	h, fr, _, trig := loopHarness(t, "go: downloading golang.org/x/text v0.14.0\ndial tcp: lookup proxy.golang.org: no such host")
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("an environment failure must not loop: %d rounds", len(fr.calls))
	}
	if hasPrefix(h.bus.History("#general", 0), "could not verify: `go test ./...` needs something this environment lacks") == nil {
		t.Fatal("environment failure must be named")
	}
	if has(h.bus.History("#general", 0), "reply 1") == nil {
		t.Fatal("the reply still lands")
	}
}

// With DHI's git seam, a turn that changed nothing in the worktree (a
// status question on the card) is not verified.
func TestLoopSkipsUnchangedWorktree(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	h, fr, _, trig := loopHarness(t, "")
	wt := worktreeOf(h)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "seed"}} {
		if out, err := exec.Command(gitBin, append([]string{"-C", wt}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	h.rt.cfg.Git = gitcore.NewRunner(gitBin, os.Environ())
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 0 {
		t.Fatalf("an unchanged worktree must not be verified: %d", len(fr.calls))
	}
}

// DHI's own write tool lands in the task worktree and, under the
// envelope, needs no approval.
func TestServedWriteLandsInWorktree(t *testing.T) {
	h, _, st, trig := loopHarness(t, "")
	task, _ := st.Get("fix-bug")
	asked := false
	h.rt.cfg.Approvals.OnRequest = func(ap *tools.Approval) { asked = true; go h.rt.cfg.Approvals.Resolve(ap.ID, true) }
	wt := worktreeOf(h)
	handler := dhitools.Deps{
		Agent: h.rt.agents["scout"].m, WS: h.ws, Approvals: h.rt.cfg.Approvals,
		Workdir: wt, Envelope: h.rt.envelopeFor(task, true, wt, Tuning{Autonomy: "envelope"}),
		EnvelopeTask: task.Slug, MemberRoots: h.rt.memberRoots(task),
	}.Handler()
	_ = trig
	out, isErr, err := handler.CallTool(context.Background(), "write", json.RawMessage(`{"path":"api/fix.go","content":"package x\n"}`))
	if err != nil || isErr {
		t.Fatalf("write = %q %v %v", out, isErr, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "fix.go")); err != nil {
		t.Fatalf("write did not land in the worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.ws.Root, "api", "fix.go")); err == nil {
		t.Fatal("write landed on the shared checkout")
	}
	if asked {
		t.Fatal("the envelope must not ask for a write inside the task worktree")
	}
}
