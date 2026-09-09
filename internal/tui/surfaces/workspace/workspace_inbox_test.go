package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/inbox"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/unread"
)

// seedInbox populates a model with one of each attention source: a pending
// approval, a failed run on an open task, an in-review task, and an
// unreplied @you mention.
func seedInbox(t *testing.T, m *Model) {
	t.Helper()
	apq := tools.NewApprovals()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go apq.Ask(ctx, "scout", sandbox.OpWrite, ".dhi/tasks/x/y.md", "policy")
	m.approvals = apq

	ts, err := tasks.Open(m.ws)
	if err != nil {
		t.Fatal(err)
	}
	ts.Create("fix-login", "Fix login race", "alice", "")
	ts.RecordRun("fix-login", tasks.Run{
		ID: "r1", Agent: "alice", Runtime: "cli:claude",
		Started: autoClock(2026, 9, 2, 8, 50), Finished: autoClock(2026, 9, 2, 9, 0),
		Status: tasks.RunTimeout, Error: "deadline", TokensIn: -1, TokensOut: -1,
	})
	ts.Create("ship-docs", "Ship docs", "scout", "")
	ts.RecordRun("ship-docs", tasks.Run{
		ID: "r2", Agent: "scout", Runtime: "cli:codex",
		Started: autoClock(2026, 9, 2, 7, 0), Finished: autoClock(2026, 9, 2, 7, 30),
		Status: tasks.RunOK, TokensIn: -1, TokensOut: -1,
	})
	ts.SetStatus("ship-docs", tasks.InReview)
	m.taskStore = ts

	if b, err := bus.Open(m.ws); err == nil {
		m.bus = b
		m.pane = newChatPane(b, m.rt, m.org)
		m.refreshPaneRail()
		// Open the read-mark store BEFORE posting: seeding marks the
		// (empty) history read, so the mention below is genuinely unread.
		us, err := unread.Open(m.ws, b)
		if err != nil {
			t.Fatal(err)
		}
		m.unreadStore = us
	}
	_, _ = m.bus.Post(bus.Message{Channel: "#general", Author: "scout",
		Text: "@you needs a decision on the plan"})
}

func gotoInbox(m *Model) {
	for i := secMembers; i < secInbox; i++ {
		m.HandleKey("]")
	}
}

func TestInboxPopulatedGolden(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	gotoInbox(m)
	items := m.inboxItems()
	if len(items) != 4 {
		t.Fatalf("got %d items", len(items))
	}
	want := []inbox.ItemKind{inbox.Approval, inbox.RunFailed, inbox.InReview, inbox.AgentMessage}
	for i, w := range want {
		if items[i].Kind != w {
			t.Fatalf("item %d kind = %v, want %v", i, items[i].Kind, w)
		}
	}
	golden.Snapshot(t, "workspace_inbox", m.View())
}

func TestInboxEmptyGolden(t *testing.T) {
	m, _ := newSurface(t)
	gotoInbox(m)
	if n := m.AttentionCount(); n != 0 {
		t.Fatalf("empty attention = %d", n)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "nothing needs attention") {
		t.Fatalf("empty state missing:\n%s", out)
	}
	golden.Snapshot(t, "workspace_inbox_empty", m.View())
}

func TestInboxNarrowWrapGolden(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	m.Resize(64, 24)
	gotoInbox(m)
	golden.Snapshot(t, "workspace_inbox_narrow", m.View())
}

func TestInboxJumpMention(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	if msg := m.bus.History("#general", 0); len(msg) == 0 {
		t.Fatal("no mention posted")
	}
	gotoInbox(m)
	// mention is the last item (rank 4).
	for i := 0; i < 3; i++ {
		m.HandleKey("j")
	}
	if !m.HandleKey("enter") {
		t.Fatal("enter not consumed")
	}
	if m.sec != secChannels {
		t.Fatalf("jumped to %v, want channels", m.sec)
	}
	if m.pane.threadID == 0 {
		t.Fatal("mention thread not opened")
	}
}

func TestInboxJumpRunFailedOpensReplay(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	gotoInbox(m)
	m.HandleKey("j") // approval -> run_failed
	if !m.HandleKey("enter") {
		t.Fatal("enter not consumed")
	}
	if m.replay == nil {
		t.Fatal("run jump did not open replay")
	}
	if m.sec != secTasks {
		t.Fatalf("run jump left sec=%v, want tasks", m.sec)
	}
	if tk := m.taskRows(); m.cursors[secTasks] >= len(tk) {
		t.Fatal("run jump left task cursor out of range")
	}
}

func TestInboxJumpApprovalSeamObserved(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	called := 0
	m.openChat = func() bool { called++; return true }
	gotoInbox(m)
	if !m.HandleKey("enter") {
		t.Fatal("enter not consumed")
	}
	if called != 1 {
		t.Fatalf("approval seam called %d times", called)
	}
}

func TestInboxJumpMissingSeamDegrades(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	gotoInbox(m)
	m.openChat = nil
	if !m.HandleKey("enter") {
		t.Fatal("enter not consumed")
	}
	if m.inboxHint != "approval jump: editor chat unavailable" {
		t.Fatalf("degrade hint = %q", m.inboxHint)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "editor chat unavailable") {
		t.Fatalf("degrade hint not visible:\n%s", out)
	}
}

func TestInboxJumpInReviewHintWhenNoReview(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	// in_review has no review svc attached → hint names the attach path.
	it := inbox.Item{Kind: inbox.InReview, TaskSlug: "ship-docs"}
	m.inboxJump(it)
	if !strings.Contains(m.inboxHint, "attach a review first") {
		t.Fatalf("hint = %q", m.inboxHint)
	}
}

func TestInboxJumpInReviewSeamRoutes(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	var got string
	m.openReview = func(id string) bool { got = id; return true }
	it := inbox.Item{Kind: inbox.InReview, TaskSlug: "ship-docs", ReviewID: "ship-docs-branch-1"}
	m.inboxJump(it)
	if got != "ship-docs-branch-1" {
		t.Fatalf("openReview got %q", got)
	}
}

func TestInboxCountClearsAfterResolve(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	if n := m.AttentionCount(); n != 4 {
		t.Fatalf("attention = %d, want 4", n)
	}
	// Resolve the approval and drop the other sources: count falls.
	m.approvals = nil
	m.taskStore = nil
	m.bus = nil
	m.pane = nil
	m.unreadStore = nil
	if n := m.AttentionCount(); n != 0 {
		t.Fatalf("resolved attention = %d, want 0", n)
	}
}
