package tasks

import (
	"testing"
	"time"
)

func mkRun(agent string, status RunStatus, in, out int, cost float64, hasCost bool) Run {
	now := time.Now().UTC()
	return Run{
		ID: "r", Agent: agent,
		Started: now.Add(-time.Minute), Finished: now,
		Status: status, TokensIn: in, TokensOut: out,
		CostUSD: cost, HasCost: hasCost,
	}
}

func TestRollupRunsMixedCostsAndTokens(t *testing.T) {
	runs := []Run{
		mkRun("alice", RunOK, 100, 50, 0.42, true),
		mkRun("alice", RunError, 200, 20, 0.00, false),  // cost-less → partial sum
		mkRun("alice", RunTimeout, -1, -1, 0.00, false), // unknown tokens → marked
		mkRun("alice", RunOK, 500, 0, 1.00, true),       // zero out is known
		mkRun("alice", RunError, 25, 75, 0.10, true),
	}
	rl := RollupRuns(runs)

	want := Rollup{
		Runs: 5, OK: 2, Fail: 2, Timeout: 1,
		TokensIn: 825, TokensOut: 145, TokRuns: 4,
		TokPartial: true,
		CostSum:    1.52, Costed: 3, CostPartial: true,
	}
	if rl != want {
		t.Errorf("RollupRuns = %+v, want %+v", rl, want)
	}
	if got := rl.CostText(); got != "cost partial" {
		t.Errorf("CostText = %q, want cost partial", got)
	}
	if got := rl.TokensText(); got != "825 in / 145 out partial" {
		t.Errorf("TokensText = %q", got)
	}
}

func TestRollupRunsExcludesUnknownsWithoutLeaking(t *testing.T) {
	runs := []Run{
		mkRun("alice", RunOK, -1, -1, 0.00, true), // tokens unknown but costed
		mkRun("alice", RunOK, 1000, 500, 0.00, true),
	}
	rl := RollupRuns(runs)
	if rl.TokensIn != 1000 || rl.TokensOut != 500 || rl.TokRuns != 1 {
		// the -1 row must not leak zeroes, only mark
		t.Errorf("tokens = %d/%d over %d runs, want 1000/500/1 (partial flagged %v)",
			rl.TokensIn, rl.TokensOut, rl.TokRuns, rl.TokPartial)
	}
	if !rl.TokPartial {
		t.Error("TokPartial = false, want true (unknown row present)")
	}
	if rl.CostPartial {
		t.Error("CostPartial = true for an all-costed set")
	}
}

func TestRollupRunsAllCostedExactSum(t *testing.T) {
	runs := []Run{
		mkRun("alice", RunOK, 100, 100, 0.01, true),
		mkRun("alice", RunError, 100, 100, 2.00, true),
	}
	rl := RollupRuns(runs)
	if rl.CostSum != 2.01 || rl.Costed != 2 || rl.CostPartial {
		t.Errorf("CostSum=%v Costed=%d partial=%v, want 2.01 2 false", rl.CostSum, rl.Costed, rl.CostPartial)
	}
	if got := rl.CostText(); got != "$2.01" {
		t.Errorf("CostText = %q, want $2.01", got)
	}
}

func TestRollupRunsNoRuns(t *testing.T) {
	rl := RollupRuns(nil)
	if rl.CostText() != "n/a" || rl.TokensText() != "n/a" {
		t.Errorf("empty rollup texts = %q / %q, want n/a", rl.CostText(), rl.TokensText())
	}
	if got := rl.Summary(); got != "0 runs · 0 ok · 0 fail · 0 timeout · n/a · n/a" {
		t.Errorf("Summary = %q", got)
	}
}

func TestTaskAndAgentRollup(t *testing.T) {
	s, _ := setupStore(t)
	if err := s.Create("ship-it", "Ship it", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("docs", "Docs", "bob", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRun("ship-it", mkRun("alice", RunOK, 100, 50, 0.42, true)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRun("ship-it", mkRun("alice", RunError, 10, 5, 0.00, false)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRun("docs", mkRun("bob", RunOK, 1, 1, 0.01, true)); err != nil {
		t.Fatal(err)
	}

	trl, err := s.TaskRollup("ship-it")
	if err != nil {
		t.Fatal(err)
	}
	if trl.Runs != 2 || trl.OK != 1 || trl.Fail != 1 || !trl.CostPartial {
		t.Errorf("TaskRollup = %+v (want Runs 2 OK 1 Fail 1 CostPartial settable)", trl)
	}

	arl := s.AgentRollup("alice")
	if arl.Runs != 2 || arl.OK != 1 || arl.Fail != 1 {
		t.Errorf("AgentRollup(alice) = %+v", arl)
	}
	brl := s.AgentRollup("bob")
	if brl.Runs != 1 || brl.OK != 1 || brl.CostSum != 0.01 {
		t.Errorf("AgentRollup(bob) = %+v", brl)
	}

	aruns := s.AgentRuns("alice")
	if len(aruns) != 2 {
		t.Errorf("AgentRuns = %d, want 2", len(aruns))
	}
	// newest first: the error run finished after the ok run
	if aruns[0].Status != RunError {
		t.Errorf("AgentRuns[0] = %+v, want newest first", aruns[0])
	}
}

func TestNewestRun(t *testing.T) {
	tk := Task{Runs: []Run{
		mkRun("alice", RunOK, 0, 0, 0, false),
	}}
	older := tk.Runs[0].Started
	newer := older.Add(time.Hour)
	tk.Runs = append(tk.Runs, Run{ID: "latest", Agent: "a",
		Started: newer, Finished: newer, Status: RunError})
	got, ok := tk.NewestRun()
	if !ok || got.ID != "latest" {
		t.Errorf("NewestRun = %+v ok=%v, want latest", got, ok)
	}
	if _, ok := (&Task{}).NewestRun(); ok {
		t.Error("NewestRun on empty task matched")
	}
}

func TestRollupRunsExactCostOn0USD(t *testing.T) {
	// a costed run can cost $0.00 (free tier) and still count costed.
	runs := []Run{mkRun("alice", RunOK, 10, 10, 0.0, true)}
	rl := RollupRuns(runs)
	if rl.Costed != 1 || rl.CostPartial || rl.CostText() != "$0" {
		t.Errorf("CostText = %q Costed=%d partial=%v", rl.CostText(), rl.Costed, rl.CostPartial)
	}
}
