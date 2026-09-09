package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// fixtureTranscript is a realistic F-013 event stream for replay goldens.
const fixtureTranscript = `{"kind":"progress","detail":"inspecting the login flow"}
{"kind":"command","detail":"git status"}
{"kind":"command","detail":"cd repos/alpha && go test ./..."}
{"kind":"error","detail":"compile error: undefined: x"}
{"kind":"progress","detail":"applying the fix"}
{"kind":"final","detail":"done · 1.2s · 1.5k in / 0.4k out · $0.42"}
`

// seededRunStore builds a workspace card carrying three alice runs (ok
// costed, error cost-less, timeout cost-less) plus a replayable
// transcript, and returns the task store wired for the surface.
func seededRunStore(t *testing.T, ws *workspace.Workspace) *tasks.Store {
	t.Helper()
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	runsDir := filepath.Join(ws.Root, ".dhi", "agents", "alice", "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(runsDir, "run-replay-test-0.jsonl")
	if err := os.WriteFile(transcript, []byte(fixtureTranscript), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.Create("fix-login", "Fix login race", "alice", "frontend"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRun("fix-login", tasks.Run{
		ID: "run-early", Agent: "alice", Runtime: "cli:claude", Model: "opus",
		Started:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Finished: time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC),
		Status:   tasks.RunTimeout, Error: "run interrupted: context deadline exceeded",
		TokensIn: -1, TokensOut: -1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRun("fix-login", tasks.Run{
		ID: "run-old", Agent: "alice", Runtime: "cli:claude", Model: "opus",
		Started:  time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		Finished: time.Date(2026, 1, 1, 1, 1, 0, 0, time.UTC),
		Status:   tasks.RunError, Error: "network",
		TokensIn: -1, TokensOut: -1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRun("fix-login", tasks.Run{
		ID: "run-replay-test", Agent: "alice", Runtime: "cli:claude", Model: "opus",
		Started:  time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC),
		Finished: time.Date(2026, 1, 1, 2, 0, 1, 200000000, time.UTC),
		Status:   tasks.RunOK, Exit: 0,
		Summary:  "Fixed it.",
		TokensIn: 100, TokensOut: 50, CostUSD: 0.42, HasCost: true,
		Transcript: transcript,
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestTasksRunsSuffixAndReplayRouting(t *testing.T) {
	m, ws := newSurface(t)
	m.taskStore = seededRunStore(t, ws)

	for i := secMembers; i < secTasks; i++ {
		m.HandleKey("]")
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "3 runs · cost partial") {
		t.Fatalf("runs suffix missing:\n%s", out)
	}

	// r opens the newest run's replay.
	m.HandleKey("r")
	if m.replay == nil {
		t.Fatal("r did not open the replay")
	}
	out = ansi.Strip(m.View())
	for _, want := range []string{"replay run-replay-test", "cli:claude/opus", "1.2s", "$0.42",
		"❯ cd repos/alpha && go test ./...", "compile error: undefined: x",
		"inspecting the login flow"} {
		if !strings.Contains(out, want) {
			t.Fatalf("replay missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "transcript unavailable") {
		t.Fatalf("replay refused a present transcript:\n%s", out[:400])
	}

	// esc returns to the deck; the list is intact underneath.
	m.HandleKey("esc")
	if m.replay != nil {
		t.Fatal("esc did not close the replay")
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "fix-login") {
		t.Fatalf("task list not restored after esc:\n%s", out)
	}
}

func TestTaskReplayGolden(t *testing.T) {
	m, ws := newSurface(t)
	m.taskStore = seededRunStore(t, ws)
	for i := secMembers; i < secTasks; i++ {
		m.HandleKey("]")
	}
	m.HandleKey("r")
	golden.Snapshot(t, "workspace_task_run_replay", m.View())
}

func TestInspectRunsSubsection(t *testing.T) {
	m, ws := newSurface(t)
	m.roster = &stubRoster{ids: []string{"alice"}}
	m.taskStore = seededRunStore(t, ws)

	for i := secMembers; i < secInspect; i++ {
		m.HandleKey("]")
	}
	m.HandleKey("enter") // open profile

	out := ansi.Strip(m.View())
	for _, want := range []string{
		"runs",
		"3 runs · 1 ok · 1 fail · 1 timeout · 100 in / 50 out partial · cost partial",
		"cli:claude/opus",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("RUNS subsection missing %q:\n%s", want, out)
		}
	}

	// e opens the newest run's replay from the run rows.
	m.HandleKey("e")
	if m.replay == nil {
		t.Fatal("e did not open the newest replay")
	}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "replay run-replay-test") {
		t.Fatalf("replay header missing newest run:\n%s", got)
	}
}

func TestInspectRunsSubsectionGolden(t *testing.T) {
	m, ws := newSurface(t)
	m.roster = &stubRoster{ids: []string{"alice"}}
	m.taskStore = seededRunStore(t, ws)
	for i := secMembers; i < secInspect; i++ {
		m.HandleKey("]")
	}
	m.HandleKey("enter")
	golden.Snapshot(t, "workspace_inspect_runs", m.View())
}

func TestReplayMissingTranscriptGolden(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create("ghost", "Gone", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRun("ghost", tasks.Run{
		ID: "run-gone", Agent: "alice", Runtime: "cli:claude",
		Started:  time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		Finished: time.Date(2026, 1, 1, 1, 0, 30, 0, time.UTC),
		Status:   tasks.RunError, Exit: 1, Error: "exit 1",
		Transcript: filepath.Join(ws.Root, ".dhi", "agents", "alice", "runs", "pruned.jsonl"),
	}); err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	for i := secMembers; i < secTasks; i++ {
		m.HandleKey("]")
	}
	m.HandleKey("r")
	golden.Snapshot(t, "workspace_replay_missing_transcript", m.View())
}

// cardsWithRuns is the store the replay routing tests share.
func cardsWithRuns(t *testing.T, ws *workspace.Workspace, runs []tasks.Run) *tasks.Store {
	t.Helper()
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create("card", "Card", "alice", ""); err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if err := store.RecordRun("card", r); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestReplayScrolls(t *testing.T) {
	m, ws := newSurface(t)
	store, err := tasks.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create("long", "Long", "alice", ""); err != nil {
		t.Fatal(err)
	}
	var long strings.Builder
	for i := 0; i < 60; i++ {
		long.WriteString(`{"kind":"progress","detail":"step `)
		long.WriteString(itoa(i))
		long.WriteString("\"}\n")
	}
	runID := filepath.Join(ws.Root, ".dhi", "agents", "alice", "runs")
	if err := os.MkdirAll(runID, 0o755); err != nil {
		t.Fatal(err)
	}
	tp := filepath.Join(runID, "run-long-0.jsonl")
	if err := os.WriteFile(tp, []byte(long.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRun("long", tasks.Run{
		ID: "run-long", Agent: "alice", Runtime: "cli:claude",
		Started:  time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		Finished: time.Date(2026, 1, 1, 1, 1, 0, 0, time.UTC),
		Status:   tasks.RunOK, Transcript: tp,
	}); err != nil {
		t.Fatal(err)
	}
	m.taskStore = store
	for i := secMembers; i < secTasks; i++ {
		m.HandleKey("]")
	}
	m.HandleKey("r")

	if m.replay.scroll != 0 {
		t.Fatalf("scroll = %d, want top", m.replay.scroll)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "step 0") {
		t.Fatalf("replay did not start at the top:\n%s", out)
	}
	for i := 0; i < 40; i++ {
		m.HandleKey("j")
	}
	if m.replay.scroll == 0 {
		t.Fatal("j did not advance the window")
	}
	// the window moved: "step 0" scrolled out
	if rolled := ansi.Strip(m.View()); strings.Contains(rolled, "step 0") {
		t.Fatalf("scroll did not move the window:\n%s", rolled)
	}
	m.HandleKey("G")
	m.HandleKey("j") // clamped at the bottom
	max := m.replay.scroll
	m.HandleKey("G")
	if m.replay.scroll != max || m.replay.scroll == 0 {
		t.Fatalf("G = %d, want clamped bottom %d", m.replay.scroll, max)
	}
	m.HandleKey("esc")
	if m.replay != nil {
		t.Fatal("esc did not close replay")
	}
}

func TestReplayRWithoutRuns(t *testing.T) {
	m, ws := newSurface(t)
	m.taskStore = cardsWithRuns(t, ws, nil)
	for i := secMembers; i < secTasks; i++ {
		m.HandleKey("]")
	}
	m.HandleKey("r")
	if m.replay != nil {
		t.Fatal("r opened a replay for a card with no runs")
	}
	if m.form.err != "card has no recorded runs" {
		t.Fatalf("flash err = %q, want refusal message", m.form.err)
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "card") {
		t.Fatal("list rendered wrong after refused r")
	}
}
