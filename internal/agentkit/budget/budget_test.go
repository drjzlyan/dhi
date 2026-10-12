package budget

import (
	"strings"
	"testing"
	"time"
)

func TestLedgerTallyAndCheck(t *testing.T) {
	l := Open(t.TempDir())
	if es, err := l.Entries(); err != nil || len(es) != 0 {
		t.Fatalf("missing ledger = %v %v", es, err)
	}
	now := time.Date(2026, 10, 13, 15, 0, 0, 0, time.Local)
	yesterday := now.Add(-24 * time.Hour)
	for _, e := range []Entry{
		{At: yesterday, Agent: "dev", Task: "fix", Tokens: 1000, CostUSD: 1.0},
		{At: now, Agent: "dev", Task: "fix", Tokens: 500, CostUSD: 0.5},
		{At: now, Agent: "qa", Tokens: 300, CostUSD: 0.3},
	} {
		if err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	es, err := l.Entries()
	if err != nil || len(es) != 3 {
		t.Fatalf("entries = %d %v", len(es), err)
	}
	s := Tally(es, "dev", "fix", DayStart(now))
	want := Spent{TaskUSD: 1.5, TaskTokens: 1500, AgentDayUSD: 0.5, WorkspaceDayUSD: 0.8}
	if s != want {
		t.Fatalf("tally = %+v, want %+v", s, want)
	}

	cases := []struct {
		lim     Limits
		hasTask bool
		ok      bool
		key     string
	}{
		{Limits{}, true, true, ""},
		{Limits{TaskUSD: 1.5}, true, false, "budgets.task_usd"},
		{Limits{TaskUSD: 1.5}, false, true, ""}, // task caps bind card turns only
		{Limits{TaskTokens: 2000}, true, true, ""},
		{Limits{TaskTokens: 1500}, true, false, "budgets.task_tokens"},
		{Limits{AgentDayUSD: 0.5}, false, false, "budgets.agent_day_usd"},
		{Limits{WorkspaceDayUSD: 1}, false, true, ""},
		{Limits{WorkspaceDayUSD: 0.8}, false, false, "budgets.workspace_day_usd"},
	}
	for _, c := range cases {
		msg, ok := Check(c.lim, s, c.hasTask)
		if ok != c.ok || !strings.Contains(msg, c.key) {
			t.Fatalf("Check(%+v, task=%v) = %q %v", c.lim, c.hasTask, msg, ok)
		}
	}
}
