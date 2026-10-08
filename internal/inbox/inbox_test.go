package inbox

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/unread"
)

func clock(y, m, d, hh, mm int) time.Time {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC)
}

func run(id string, st tasks.RunStatus, start, end time.Time) tasks.Run {
	return tasks.Run{ID: id, Agent: "codex", Status: st, Started: start, Finished: end}
}

// msg is a read-mark item for one agent message (the store's predicate
// already decided it is addressed to the human).
func msg(id int64, channel, author, text string, at time.Time) unread.Item {
	return unread.Item{Msg: bus.Message{ID: id, Channel: channel, Author: author, Text: text, At: at}}
}

func TestBuildApprovalRow(t *testing.T) {
	ap := &tools.Approval{ID: 7, Agent: "scout", Op: sandbox.OpWrite, Target: ".dhi/tasks/a/b.md"}
	items := Build([]*tools.Approval{ap}, nil, nil, nil)
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	it := items[0]
	if it.Kind != Approval || it.ApprovalID != 7 {
		t.Fatalf("kind/id = %v/%d", it.Kind, it.ApprovalID)
	}
	if it.Row != "approve  scout: write .dhi/tasks/a/b.md" {
		t.Fatalf("row = %q", it.Row)
	}
}

func TestBuildAgentMessageRow(t *testing.T) {
	msgs := []unread.Item{
		msg(3, "dm:scout", "scout", "heads-up, contract renewal", clock(2026, 9, 2, 8, 0)),
		msg(5, "#general", "scout", "@you needs a decision on the plan", clock(2026, 9, 2, 8, 5)),
	}
	items := Build(nil, msgs, nil, nil)
	if len(items) != 2 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].Kind != AgentMessage || items[0].MsgID != 3 || items[0].ThreadRoot != 3 {
		t.Fatalf("dm item = %+v", items[0])
	}
	if items[1].Channel != "#general" || items[1].MsgID != 5 || items[1].ThreadRoot != 5 {
		t.Fatalf("mention item = %+v", items[1])
	}
	if items[0].Row != `dm:scout  scout: "heads-up, contract renewal"` {
		t.Fatalf("dm row = %q", items[0].Row)
	}
	if items[1].Row != `#general  scout: "@you needs a decision on the plan"` {
		t.Fatalf("mention row = %q", items[1].Row)
	}
}

func TestBuildRunFailedAndInReviewRows(t *testing.T) {
	ts := []tasks.Task{
		{Slug: "fix-login", Status: tasks.Active, Assignee: "codex",
			UpdatedAt: clock(2026, 9, 2, 9, 0),
			Runs: []tasks.Run{run("r1", tasks.RunError,
				clock(2026, 9, 2, 8, 50), clock(2026, 9, 2, 9, 0))}},
		{Slug: "ship-docs", Status: tasks.InReview, Assignee: "scout",
			UpdatedAt: clock(2026, 9, 2, 8, 0),
			Runs: []tasks.Run{run("r2", tasks.RunOK,
				clock(2026, 9, 2, 7, 0), clock(2026, 9, 2, 7, 30))}},
	}
	items := Build(nil, nil, ts, nil)
	if len(items) != 2 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].Kind != RunFailed || items[0].Row != "run failed  fix-login (codex) — error after 10.0m" {
		t.Fatalf("run_failed = %+v row %q", items[0], items[0].Row)
	}
	if items[1].Kind != InReview {
		t.Fatalf("second kind = %v", items[1].Kind)
	}
	if items[1].Row != "in review  ship-docs — scout, 1 run · cost partial" {
		t.Fatalf("in_review row = %q", items[1].Row)
	}
}

func TestBuildSkipsDoneTasksAndOkRuns(t *testing.T) {
	ts := []tasks.Task{
		{Slug: "done-fail", Status: tasks.Done,
			Runs: []tasks.Run{run("r1", tasks.RunError,
				clock(2026, 9, 2, 8, 0), clock(2026, 9, 2, 8, 10))}},
		{Slug: "ok-run", Status: tasks.Active,
			Runs: []tasks.Run{run("r2", tasks.RunOK,
				clock(2026, 9, 2, 8, 0), clock(2026, 9, 2, 8, 10))}},
	}
	if items := Build(nil, nil, ts, nil); len(items) != 0 {
		t.Fatalf("expected no items, got %+v", items)
	}
}

func TestSnoozedCarriedThrough(t *testing.T) {
	until := clock(2026, 9, 3, 9, 0)
	um := msg(4, "dm:muse", "muse", "later", clock(2026, 9, 2, 9, 0))
	um.Snoozed = until
	items := Build(nil, []unread.Item{um}, nil, nil)
	if len(items) != 1 {
		t.Fatalf("got %d", len(items))
	}
	if items[0].Snoozed.IsZero() || !items[0].Snoozed.Equal(until) {
		t.Fatalf("snooze not carried: %+v", items[0])
	}
}

func TestOrderBySeverityThenAge(t *testing.T) {
	appr := []*tools.Approval{
		{ID: 1, Agent: "scout", Op: sandbox.OpExec, Target: "go build ./..."},
		{ID: 2, Agent: "muse", Op: sandbox.OpWrite, Target: ".dhi/tasks/x.md"},
	}
	msgs := []unread.Item{
		msg(10, "#general", "scout", "@you very old mention", clock(2026, 9, 2, 6, 0)),
	}
	ts := []tasks.Task{
		{Slug: "zz", Status: tasks.InReview, Assignee: "scout",
			UpdatedAt: clock(2026, 9, 2, 7, 0)},
		{Slug: "aa", Status: tasks.Active,
			UpdatedAt: clock(2026, 9, 2, 5, 0),
			Runs: []tasks.Run{run("r1", tasks.RunTimeout,
				clock(2026, 9, 2, 4, 0), clock(2026, 9, 2, 4, 10))}},
	}
	items := Build(appr, msgs, ts, nil)
	got := []ItemKind{items[0].Kind, items[1].Kind, items[2].Kind, items[3].Kind, items[4].Kind}
	want := []ItemKind{Approval, Approval, RunFailed, InReview, AgentMessage}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if items[0].ApprovalID != 1 || items[1].ApprovalID != 2 {
		t.Fatalf("approvals not oldest-first: %d, %d", items[0].ApprovalID, items[1].ApprovalID)
	}
}

func TestResolveRemovesRow(t *testing.T) {
	appr := []*tools.Approval{{ID: 5, Agent: "scout", Op: sandbox.OpRead, Target: "README.md"}}
	if items := Build(appr, nil, nil, nil); len(items) != 1 {
		t.Fatalf("before resolve: %d", len(items))
	}
	if items := Build(nil, nil, nil, nil); len(items) != 0 {
		t.Fatalf("resolved approval still listed: %+v", items)
	}
}

func TestMessageQuoteTruncated(t *testing.T) {
	long := strings.Repeat("decide ", 40) // > 96 runes
	msgs := []unread.Item{msg(1, "dm:muse", "muse", long, clock(2026, 9, 2, 9, 0))}
	items := Build(nil, msgs, nil, nil)
	if len(items) != 1 {
		t.Fatalf("got %d", len(items))
	}
	if !strings.HasSuffix(items[0].Row, "…\"") {
		t.Fatalf("row not truncated: %q (len %d)", items[0].Row, len(items[0].Row))
	}
}

func TestDependencyProposalItem(t *testing.T) {
	tk := tasks.Task{Slug: "feat", Title: "Feat", Status: tasks.Active,
		Propagations: []tasks.Propagation{
			{FromMember: "api", ToMember: "web", Kind: "api", Decision: tasks.PropPending},
			{FromMember: "api", ToMember: "cli", Kind: "build", Decision: tasks.PropDeclined},
		}}
	items := Build(nil, nil, []tasks.Task{tk}, nil)
	var deps []Item
	for _, it := range items {
		if it.Kind == Dependency {
			deps = append(deps, it)
		}
	}
	if len(deps) != 1 || deps[0].ToMember != "web" || deps[0].DepKind != "api" {
		t.Fatalf("dependency items = %+v", deps)
	}
	if !strings.Contains(deps[0].Row, "api → web") {
		t.Fatalf("row = %q", deps[0].Row)
	}
}

func TestIdeationProposalItem(t *testing.T) {
	props := []ideation.Proposal{
		{ID: 3, Caller: "scout", Name: "Auth review", Mode: ideation.ModeGroup,
			Decision: ideation.ProposalPending, At: time.Unix(100, 0)},
	}
	items := Build(nil, nil, nil, props)
	if len(items) != 1 || items[0].Kind != Proposal {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if it.ProposalID != 3 || it.ProposalName != "Auth review" || it.ProposalCaller != "scout" {
		t.Fatalf("proposal item = %+v", it)
	}
	if !strings.Contains(it.Row, "scout") || !strings.Contains(it.Row, "Auth review") ||
		!strings.Contains(it.Row, "a session") {
		t.Fatalf("row = %q", it.Row)
	}
	// Proposals rank below approvals but above failed runs.
	ap := &tools.Approval{ID: 1, Agent: "a", Op: sandbox.OpWrite, Target: "x"}
	both := Build([]*tools.Approval{ap}, nil, nil, props)
	if len(both) != 2 || both[0].Kind != Approval || both[1].Kind != Proposal {
		t.Fatalf("rank order = %+v", both)
	}
}
