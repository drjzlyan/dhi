package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// sessionStub records each spawn's argv (one arg per line) as
// $DUMPDIR/spawn-<n> and reports session "S1". With $DUMPDIR/dead
// present, a resumed spawn fails like an expired session.
const sessionStub = `#!/bin/sh
N=0
if [ -f "$DUMPDIR/n" ]; then N=$(cat "$DUMPDIR/n"); fi
N=$((N+1))
printf '%s\n' "$N" > "$DUMPDIR/n"
for a in "$@"; do printf '%s\n' "$a"; done > "$DUMPDIR/spawn-$N"
case " $* " in
  *" --resume "*)
    if [ -f "$DUMPDIR/dead" ]; then
      echo '{"type":"result","subtype":"success","is_error":true,"result":"No conversation found with session ID: S1"}'
      exit 1
    fi;;
esac
echo '{"type":"result","subtype":"success","is_error":false,"result":"reply '$N'","session_id":"S1","usage":{"input_tokens":2,"output_tokens":1}}'
`

func spawnArgs(t *testing.T, h *harness, n int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.dumpDir, "spawn-"+string(rune('0'+n))))
	if err != nil {
		t.Fatalf("spawn %d: %v", n, err)
	}
	return string(b)
}

func episodeHarness(t *testing.T) *harness {
	h := newHarnessScript(t, baseDoc(), sessionStub)
	h.rt.cfg.Episodes = episode.Open(h.ws.Root)
	return h
}

// F-066: the second turn of a thread resumes the CLI session and sends
// only what the session has not seen.
func TestSecondTurnResumesSession(t *testing.T) {
	h := episodeHarness(t)
	root := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout first ask: remember kiwi"})
	if err := h.rt.Turn(context.Background(), "scout", root); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spawnArgs(t, h, 1), "--resume") {
		t.Fatal("a first turn must start fresh")
	}
	next := mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: bus.Human, Text: "@scout second ask"})
	if err := h.rt.Turn(context.Background(), "scout", next); err != nil {
		t.Fatal(err)
	}
	args := spawnArgs(t, h, 2)
	if !strings.Contains(args, "--resume\nS1\n") {
		t.Fatalf("second turn did not resume:\n%s", args)
	}
	if strings.Contains(args, "remember kiwi") {
		t.Fatalf("a resumed turn must not replay what the session saw:\n%s", args)
	}
	ep, ok, _ := h.rt.cfg.Episodes.Get("scout", episode.Key("", "#general", root.ID))
	if !ok || ep.Turns != 2 || ep.Session != "S1" {
		t.Fatalf("episode = %+v", ep)
	}
}

// A dead session falls back to one fresh spawn — named, and without
// spending the (zero) retry budget.
func TestDeadSessionFallsBackFresh(t *testing.T) {
	h := episodeHarness(t)
	root := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout one"})
	if err := h.rt.Turn(context.Background(), "scout", root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dumpDir, "dead"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	next := mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: bus.Human, Text: "@scout two"})
	if err := h.rt.Turn(context.Background(), "scout", next); err != nil {
		t.Fatalf("fallback must recover the turn: %v", err)
	}
	if args := spawnArgs(t, h, 3); strings.Contains(args, "--resume") || !strings.Contains(args, "@scout one") {
		t.Fatalf("fallback spawn must be fresh with the full history:\n%s", args)
	}
	if hasPrefix(h.bus.History("#general", root.ID), "could not continue the earlier CLI session") == nil {
		t.Fatal("fallback must be named in the thread")
	}
}

func TestResumeOffNeverResumes(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Resume: false} }
	root := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout one"})
	_ = h.rt.Turn(context.Background(), "scout", root)
	next := mustPost(t, h, bus.Message{Channel: "#general", Thread: root.ID, Author: bus.Human, Text: "@scout two"})
	_ = h.rt.Turn(context.Background(), "scout", next)
	args := spawnArgs(t, h, 2)
	if strings.Contains(args, "--resume") || !strings.Contains(args, "This work so far: 1 earlier turn") {
		t.Fatalf("resume off: fresh spawn with the episode summary expected:\n%s", args)
	}
}

func TestMaxTurnsReachesSpawn(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Tuning = func() Tuning { return Tuning{MaxTurns: 7} }
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	_ = h.rt.Turn(context.Background(), "scout", trig)
	if !strings.Contains(spawnArgs(t, h, 1), "--max-turns\n7\n") {
		t.Fatal("agents.max_turns did not reach the CLI")
	}
}

// F-066: a spent budget refuses the spawn by name; nothing runs.
func TestBudgetRefusesSpawn(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Spend = budget.Open(h.ws.Root)
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Budgets: budget.Limits{AgentDayUSD: 1}} }
	if err := h.rt.cfg.Spend.Append(budget.Entry{At: time.Now(), Agent: "scout", CostUSD: 1.2}); err != nil {
		t.Fatal(err)
	}
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	err := h.rt.Turn(context.Background(), "scout", trig)
	if err == nil || !strings.Contains(err.Error(), "budgets.agent_day_usd") {
		t.Fatalf("turn must refuse on budget, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(h.dumpDir, "n")); statErr == nil {
		t.Fatal("a refused turn must not spawn")
	}
	if hasPrefix(h.bus.History("#general", 0), "budget reached") == nil {
		t.Fatal("budget refusal must be named in the thread")
	}
}

func TestRunsLandInLedger(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Spend = budget.Open(h.ws.Root)
	trig := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout go"})
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	es, err := h.rt.cfg.Spend.Entries()
	if err != nil || len(es) != 1 || es[0].Agent != "scout" || es[0].Tokens != 3 {
		t.Fatalf("ledger = %+v %v", es, err)
	}
}

// F-066: the feature workflow rides task turns only.
func TestWorkflowOnlyForTaskTurns(t *testing.T) {
	h := episodeHarness(t)
	h.rt.cfg.Workflows = true
	st, err := tasks.Open(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	h.rt.cfg.Tasks = st
	chat := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout what does main do?"})
	_ = h.rt.Turn(context.Background(), "scout", chat)
	if strings.Contains(spawnArgs(t, h, 1), "Feature workflow") {
		t.Fatal("a chat turn must not carry the feature workflow")
	}
	work := mustPost(t, h, bus.Message{Channel: "#general", Author: bus.Human, Text: "@scout build it"})
	if err := st.Create("build-it", "Build it", "scout", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.BindThread("build-it", "#general", work.ID); err != nil {
		t.Fatal(err)
	}
	_ = h.rt.Turn(context.Background(), "scout", work)
	if !strings.Contains(spawnArgs(t, h, 2), "Feature workflow") {
		t.Fatal("a task turn must carry the feature workflow")
	}
}
