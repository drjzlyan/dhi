package inbox

import (
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func clock(y, m, d, hh, mm int) time.Time {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC)
}

func run(id string, st tasks.RunStatus, start, end time.Time) tasks.Run {
	return tasks.Run{ID: id, Agent: "codex", Status: st, Started: start, Finished: end}
}

func testBus(t *testing.T) *bus.Bus {
	t.Helper()
	b, err := bus.Open(&workspace.Workspace{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func post(t *testing.T, b *bus.Bus, msg bus.Message) bus.Message {
	t.Helper()
	got, err := b.Post(msg)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBuildApprovalRow(t *testing.T) {
	ap := &tools.Approval{ID: 7, Agent: "scout", Op: sandbox.OpWrite, Target: ".dhi/tasks/a/b.md"}
	items := Build([]*tools.Approval{ap}, nil, nil)
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

func TestBuildRunFailedAndInReviewRows(t *testing.T) {
	tasks_ := []tasks.Task{
		{Slug: "fix-login", Status: tasks.Active, Assignee: "codex",
			UpdatedAt: clock(2026, 9, 2, 9, 0),
			Runs: []tasks.Run{run("r1", tasks.RunError,
				clock(2026, 9, 2, 8, 50), clock(2026, 9, 2, 9, 0))}},
		{Slug: "ship-docs", Status: tasks.InReview, Assignee: "scout",
			UpdatedAt: clock(2026, 9, 2, 8, 0),
			Runs: []tasks.Run{run("r2", tasks.RunOK,
				clock(2026, 9, 2, 7, 0), clock(2026, 9, 2, 7, 30))}},
	}
	items := Build(nil, nil, tasks_)
	if len(items) != 2 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].Kind != RunFailed || items[0].Row != "run failed  fix-login (codex) — error after 10.0m" {
		t.Fatalf("run_failed = %+v row %q", items[0], items[0].Row)
	}
	if items[1].Kind != InReview {
		t.Fatalf("second kind = %v", items[1].Kind)
	}
	if items[1].Row != "in review  ship-docs — scout, 1 runs · cost partial" {
		t.Fatalf("in_review row = %q", items[1].Row)
	}
}

func TestBuildSkipsDoneTasksAndOkRuns(t *testing.T) {
	tasks_ := []tasks.Task{
		{Slug: "done-fail", Status: tasks.Done,
			Runs: []tasks.Run{run("r1", tasks.RunError,
				clock(2026, 9, 2, 8, 0), clock(2026, 9, 2, 8, 10))}},
		{Slug: "ok-run", Status: tasks.Active,
			Runs: []tasks.Run{run("r2", tasks.RunOK,
				clock(2026, 9, 2, 8, 0), clock(2026, 9, 2, 8, 10))}},
	}
	if items := Build(nil, nil, tasks_); len(items) != 0 {
		t.Fatalf("expected no items, got %+v", items)
	}
}

func TestMentionRule(t *testing.T) {
	b := testBus(t)
	root := post(t, b, bus.Message{Channel: "#general", Author: "scout", Text: "@you needs a decision on the plan"})
	post(t, b, bus.Message{Channel: "#general", Author: "you", Text: "on it", Thread: root.ID})

	// Unreplied DM mention is included.
	dm := post(t, b, bus.Message{Channel: "dm:scout", Author: "scout", Text: "@you heads-up, contract renewal"})

	// A plain message without @you is never a mention.
	post(t, b, bus.Message{Channel: "#general", Author: "muse", Text: "pushed a branch"})

	items := Build(nil, b, nil)
	var mentions []Item
	for _, it := range items {
		if it.Kind == Mention {
			mentions = append(mentions, it)
		}
	}
	if len(mentions) != 1 {
		t.Fatalf("got %d mentions: %+v", len(mentions), mentions)
	}
	it := mentions[0]
	if it.Channel != "dm:scout" || it.ThreadRoot != dm.ID || it.MsgID != dm.ID {
		t.Fatalf("mention = %+v", it)
	}
	if !strings.Contains(it.Row, `dm:scout  scout: "`) {
		t.Fatalf("row = %q", it.Row)
	}

	// The human replying into the DM thread closes the mention.
	post(t, b, bus.Message{Channel: "dm:scout", Author: "you", Text: "noted", Thread: dm.ID})
	if items := Build(nil, b, nil); len(items) != 0 {
		t.Fatalf("mention not closed after threaded you reply: %+v", items)
	}
}

func TestMentionRepliedExcludedThreaded(t *testing.T) {
	b := testBus(t)
	root := post(t, b, bus.Message{Channel: "#design", Author: "muse", Text: "@you thoughts?"})
	post(t, b, bus.Message{Channel: "#design", Author: "you", Text: "landed", Thread: root.ID})
	if items := Build(nil, b, nil); len(items) != 0 {
		t.Fatalf("replied mention still present: %+v", items)
	}
}

func TestMentionWithinThreadRequiresLaterYou(t *testing.T) {
	b := testBus(t)
	root := post(t, b, bus.Message{Channel: "#design", Author: "muse", Text: "initial plan"})
	post(t, b, bus.Message{Channel: "#design", Author: "muse", Text: "@you see the follow-up?", Thread: root.ID})
	// muse, not you, answers → still open.
	post(t, b, bus.Message{Channel: "#design", Author: "scout", Text: "+1", Thread: root.ID})
	items := Build(nil, b, nil)
	if len(items) != 1 || items[0].Kind != Mention {
		t.Fatalf("expected the threaded mention open: %+v", items)
	}
	if items[0].ThreadRoot != root.ID || items[0].MsgID == root.ID {
		t.Fatalf("jump payload = %+v", items[0])
	}

	// The human then replies → mention closes.
	post(t, b, bus.Message{Channel: "#design", Author: "you", Text: "looks good", Thread: root.ID})
	if items := Build(nil, b, nil); len(items) != 0 {
		t.Fatalf("mention not closed after you reply: %+v", items)
	}
}

func TestOrderBySeverityThenAge(t *testing.T) {
	b := testBus(t)
	post(t, b, bus.Message{Channel: "#general", Author: "scout", Text: "@you very old mention"})

	appr := []*tools.Approval{
		{ID: 1, Agent: "scout", Op: sandbox.OpExec, Target: "go build ./..."},
		{ID: 2, Agent: "muse", Op: sandbox.OpWrite, Target: ".dhi/tasks/x.md"},
	}
	tasks_ := []tasks.Task{
		{Slug: "zz", Status: tasks.InReview, Assignee: "scout",
			UpdatedAt: clock(2026, 9, 2, 7, 0)},
		{Slug: "aa", Status: tasks.Active,
			UpdatedAt: clock(2026, 9, 2, 5, 0),
			Runs: []tasks.Run{run("r1", tasks.RunTimeout,
				clock(2026, 9, 2, 4, 0), clock(2026, 9, 2, 4, 10))}},
	}
	items := Build(appr, b, tasks_)
	got := []ItemKind{items[0].Kind, items[1].Kind, items[2].Kind, items[3].Kind, items[4].Kind}
	want := []ItemKind{Approval, Approval, RunFailed, InReview, Mention}
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
	if items := Build(appr, nil, nil); len(items) != 1 {
		t.Fatalf("before resolve: %d", len(items))
	}
	if items := Build(nil, nil, nil); len(items) != 0 {
		t.Fatalf("resolved approval still listed: %+v", items)
	}
}

func TestMentionQuoteTruncated(t *testing.T) {
	b := testBus(t)
	long := strings.Repeat("decide ", 40) // > 96 runes
	post(t, b, bus.Message{Channel: "dm:muse", Author: "muse", Text: "@you " + long})
	items := Build(nil, b, nil)
	if len(items) != 1 {
		t.Fatalf("got %d", len(items))
	}
	if !strings.HasSuffix(items[0].Row, "…\"") {
		t.Fatalf("row not truncated: %q (len %d)", items[0].Row, len(items[0].Row))
	}
}
