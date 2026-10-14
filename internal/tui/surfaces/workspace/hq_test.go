package workspace

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/budget"
	"github.com/drjzlyan/dhi/internal/agentkit/episode"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/lessons"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func samplePerf() Perf {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)
	runs := []tasks.Run{{Status: tasks.RunOK}, {Status: tasks.RunOK}, {Status: tasks.RunOK}, {Status: tasks.RunError}}
	eps := []episode.Episode{{Verify: "pass"}, {Verify: "pass"}, {Verify: "fail"}}
	ledger := []budget.Entry{
		{At: now, Agent: "dev", CostUSD: 0.4},
		{At: now.Add(-24 * time.Hour), Agent: "dev", CostUSD: 0.2},
		{At: now.Add(-6 * 24 * time.Hour), Agent: "dev", CostUSD: 0.6},
		{At: now.Add(-9 * 24 * time.Hour), Agent: "dev", CostUSD: 9}, // older than the week
		{At: now, Agent: "qa", CostUSD: 5},                           // someone else
	}
	ls := []lessons.Lesson{
		{Scope: lessons.ScopePrivate, Status: lessons.StatusPinned},
		{Scope: lessons.ScopePrivate, Status: lessons.StatusActive},
		{Scope: lessons.ScopePrivate, Status: lessons.StatusRetired},
		{Scope: lessons.ScopeUser, Status: lessons.StatusActive},
	}
	forged := []forge.Tool{{Name: "a", ProposedBy: "dev"}, {Name: "b", ProposedBy: "qa"}}
	return buildPerf("dev", runs, 3, eps, ledger, ls, forged, now)
}

// F-071 Agent HQ: the record counts only this agent, only this week.
func TestBuildPerf(t *testing.T) {
	p := samplePerf()
	if p.Runs != 4 || p.RunsOK != 3 || p.Verified != 2 || p.Episodes != 3 || p.TasksDone != 3 {
		t.Fatalf("counts = %+v", p)
	}
	if p.SpendWeek < 1.19 || p.SpendWeek > 1.21 || p.Spend7[6] != 0.4 || p.Spend7[0] != 0.6 {
		t.Fatalf("spend = %v %v", p.SpendWeek, p.Spend7)
	}
	if p.Lessons != 2 || p.Pinned != 1 || p.Tools != 1 {
		t.Fatalf("growth = %+v", p)
	}
}

func TestPerfLinesGolden(t *testing.T) {
	theme.SwapForTest(t, theme.Dark())
	lines := perfLines(samplePerf())
	golden.Snapshot(t, "agent_hq_perf", strings.Join(lines, "\n"))
	if got := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(got, "3 tasks shipped · 75% runs ok · 2/3 verified") {
		t.Fatalf("perf:\n%s", got)
	}
}
