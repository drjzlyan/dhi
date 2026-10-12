package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
)

// learnStub answers the reflection prompt with one lesson (and counts
// reflections); any other prompt is a normal session turn.
const learnStub = `#!/bin/sh
N=0
if [ -f "$DUMPDIR/n" ]; then N=$(cat "$DUMPDIR/n"); fi
N=$((N+1))
printf '%s\n' "$N" > "$DUMPDIR/n"
for a in "$@"; do printf '%s\n' "$a"; done > "$DUMPDIR/spawn-$N"
case "$2" in
  *"reviewing your own just-finished work"*)
    R=0; if [ -f "$DUMPDIR/reflections" ]; then R=$(cat "$DUMPDIR/reflections"); fi
    printf '%s\n' "$((R+1))" > "$DUMPDIR/reflections"
    echo '{"type":"result","subtype":"success","is_error":false,"result":"{\"lessons\":[{\"text\":\"Distribute the remainder one cent at a time\",\"tags\":[\"money\"],\"scope\":\"private\",\"confidence\":0.8}]}","usage":{"input_tokens":3,"output_tokens":2},"total_cost_usd":0.001}'
    exit 0;;
esac
echo '{"type":"result","subtype":"success","is_error":false,"result":"reply '$N'","session_id":"S1","total_cost_usd":0.4,"usage":{"input_tokens":2,"output_tokens":1}}'
`

func reflections(t *testing.T, h *harness) int {
	t.Helper()
	if _, err := os.Stat(filepath.Join(h.dumpDir, "reflections")); err != nil {
		return 0
	}
	return readIntFile(t, filepath.Join(h.dumpDir, "reflections"))
}

func learnHarness(t *testing.T, outs ...string) (*harness, bus.Message) {
	t.Helper()
	h, _, _, trig := loopHarness(t, outs...)
	if err := os.WriteFile(filepath.Join(h.binDir, "claude"), []byte(learnStub), 0o755); err != nil {
		t.Fatal(err)
	}
	h.rt.cfg.Lessons = lessons.Open(h.ws.Root)
	return h, trig
}

// F-068: a verify failure is learned from once, with run provenance,
// and named in the thread.
func TestVerifyFailureIsLearned(t *testing.T) {
	h, trig := learnHarness(t, "--- FAIL: TestSplit want 334 got 333", "")
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if got := reflections(t, h); got != 1 {
		t.Fatalf("reflections = %d, want 1", got)
	}
	ls, _ := h.rt.cfg.Lessons.List("scout")
	if len(ls) != 1 || ls[0].Text != "Distribute the remainder one cent at a time" || ls[0].Source != lessons.SourceReflection || len(ls[0].Runs) != 2 {
		t.Fatalf("lessons = %+v", ls)
	}
	if hasPrefix(h.bus.History("#general", 0), "learned: Distribute the remainder") == nil {
		t.Fatal("learning must be named in the thread")
	}
	// The reflection spawn is tool-less and capped at one turn.
	refl := spawnArgs(t, h, 3)
	if !strings.Contains(refl, "--max-turns\n1\n") || strings.Contains(refl, "--mcp-config") {
		t.Fatalf("reflection spawn args:\n%s", refl)
	}
	// What was learned rides the next prompt.
	next := mustPost(t, h, bus.Message{Channel: "#general", Thread: trig.ID, Author: bus.Human, Text: "@scout split 1000 among 3 again"})
	_ = h.rt.Turn(context.Background(), "scout", next)
	if !strings.Contains(spawnArgs(t, h, 4), "Lessons you have learned from past work (apply them):\n- Distribute the remainder one cent at a time") {
		t.Fatalf("lesson not injected:\n%s", spawnArgs(t, h, 4))
	}
}

func TestCleanTurnDoesNotReflect(t *testing.T) {
	h, trig := learnHarness(t, "")
	if err := h.rt.Turn(context.Background(), "scout", trig); err != nil {
		t.Fatal(err)
	}
	if got := reflections(t, h); got != 0 {
		t.Fatalf("a clean turn reflected %d times", got)
	}
}

func TestCorrectionIsLearned(t *testing.T) {
	h, trig := learnHarness(t, "")
	_ = h.rt.Turn(context.Background(), "scout", trig) // reply 1
	fix := mustPost(t, h, bus.Message{Channel: "#general", Thread: trig.ID, Author: bus.Human, Text: "@scout no, don't change the tests"})
	if err := h.rt.Turn(context.Background(), "scout", fix); err != nil {
		t.Fatal(err)
	}
	if got := reflections(t, h); got != 1 {
		t.Fatalf("a correction must trigger reflection once, got %d", got)
	}
	ls, _ := h.rt.cfg.Lessons.List("scout")
	if len(ls) != 1 || ls[0].Source != lessons.SourceCorrection {
		t.Fatalf("lessons = %+v", ls)
	}
	refl, _ := os.ReadFile(filepath.Join(h.dumpDir, "spawn-3"))
	if !strings.Contains(string(refl), "The human corrected you:\nno, don't change the tests") || !strings.Contains(string(refl), "your earlier reply was: reply 1") {
		t.Fatalf("correction evidence missing:\n%s", refl)
	}
}

func TestReflectOffAndBudget(t *testing.T) {
	h, trig := learnHarness(t, "fail once", "")
	h.rt.cfg.Tuning = func() Tuning { return Tuning{Resume: true, MaxIterations: 3, Reflect: false} }
	_ = h.rt.Turn(context.Background(), "scout", trig)
	if reflections(t, h) != 0 {
		t.Fatal("agents.reflect = false must not reflect")
	}

	// Each turn costs $0.40 against a $0.50 daily cap: the correcting turn
	// starts under budget, and by reflection time the ledger is over it.
	h2, trig2 := learnHarness(t, "")
	h2.rt.cfg.Spend = budget.Open(h2.ws.Root)
	h2.rt.cfg.Tuning = func() Tuning {
		return Tuning{Resume: true, MaxIterations: 3, Reflect: true, Budgets: budget.Limits{AgentDayUSD: 0.5}}
	}
	_ = h2.rt.Turn(context.Background(), "scout", trig2)
	fix := mustPost(t, h2, bus.Message{Channel: "#general", Thread: trig2.ID, Author: bus.Human, Text: "@scout no, wrong file"})
	_ = h2.rt.Turn(context.Background(), "scout", fix)
	if reflections(t, h2) != 0 || hasPrefix(h2.bus.History("#general", trig2.ID), "skipped learning from this turn") == nil {
		t.Fatal("reflection must respect the budget, by name")
	}
}

// Pinned lessons always ride the prompt; user preferences are labelled.
func TestPinnedAndUserLessonsInPrompt(t *testing.T) {
	h, trig := learnHarness(t, "")
	st := h.rt.cfg.Lessons
	l, _, _ := st.Add(lessons.Lesson{Agent: "scout", Text: "Never touch the vendor directory", Confidence: 0.5})
	_ = st.Set(lessons.ScopePrivate, "scout", l.ID, func(x *lessons.Lesson) { x.Status = lessons.StatusPinned })
	_, _, _ = st.Add(lessons.Lesson{Scope: lessons.ScopeUser, Text: "Keep the fix minimal", Confidence: 0.9})
	_ = h.rt.Turn(context.Background(), "scout", trig)
	args := spawnArgs(t, h, 1)
	if !strings.Contains(args, "- Never touch the vendor directory") || !strings.Contains(args, "- Keep the fix minimal (the human's preference)") {
		t.Fatalf("lessons block:\n%s", args)
	}
}
