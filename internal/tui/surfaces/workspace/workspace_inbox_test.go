package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

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
		m.wireUnreadSeams()
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
	for m.sec != secInbox {
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
	if m.sec != secBoard {
		t.Fatalf("run jump left sec=%v, want board", m.sec)
	}
	if _, ok := m.boardSelected(m.boardGroups()); !ok {
		t.Fatal("run jump left board cursor out of range")
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

func TestInboxJumpMentionResolves(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	gotoInbox(m)
	for i := 0; i < 3; i++ {
		m.HandleKey("j")
	}
	if !m.HandleKey("enter") {
		t.Fatal("enter not consumed")
	}
	if m.sec != secChannels {
		t.Fatalf("jumped to %v, want channels", m.sec)
	}
	// F-017: the jump marked the thread read — the row is gone on the
	// next aggregation.
	items := m.inboxItems()
	for _, it := range items {
		if it.Kind == inbox.AgentMessage {
			t.Fatalf("mention row survived the jump: %+v", it)
		}
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (approval/run_failed/in_review)", len(items))
	}
}

func gotoChannels(m *Model) {
	for m.sec != secChannels {
		m.HandleKey("]")
	}
}

func TestChannelsRailUnreadGolden(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	// A second unread mention makes the rail marker counted (●2).
	if _, err := m.bus.Post(bus.Message{Channel: "#general", Author: "muse",
		Text: "@you second decision"}); err != nil {
		t.Fatal(err)
	}
	gotoChannels(m)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "●2") {
		t.Fatalf("rail marker missing:\n%s", out)
	}
	golden.Snapshot(t, "workspace_channels_unread", m.View())
}

func TestParseSnoozePreset(t *testing.T) {
	now := autoClock(2026, 9, 2, 9, 0)
	cases := []struct {
		preset string
		want   time.Time
	}{
		{"15m", now.Add(15 * time.Minute)},
		{"1h", now.Add(time.Hour)},
		{"4h", now.Add(4 * time.Hour)},
		{"tomorrow 09:00", autoClock(2026, 9, 3, 9, 0)},
	}
	for _, c := range cases {
		got, err := parseSnoozePreset(c.preset, now)
		if err != nil || !got.Equal(c.want) {
			t.Fatalf("%s: got %v err %v, want %v", c.preset, got, err, c.want)
		}
	}
	// Day-wrap: 23:00 → tomorrow 09:00.
	late := autoClock(2026, 9, 2, 23, 0)
	got, err := parseSnoozePreset("tomorrow 09:00", late)
	if err != nil || !got.Equal(autoClock(2026, 9, 3, 9, 0)) {
		t.Fatalf("day wrap: got %v err %v", got, err)
	}
	// Unknown preset names the value and the valid set.
	if _, err := parseSnoozePreset("2h", now); err == nil ||
		!strings.Contains(err.Error(), `"2h"`) || !strings.Contains(err.Error(), "15m") {
		t.Fatalf("unknown preset err = %v", err)
	}
}

func TestSnoozeFormFlow(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	m.now = func() time.Time { return autoClock(2026, 9, 2, 9, 0) }
	gotoInbox(m)
	for i := 0; i < 3; i++ {
		m.HandleKey("j") // cursor → the agent message
	}
	if !m.HandleKey("z") {
		t.Fatal("z not consumed")
	}
	if m.form.kind != fSnooze || len(m.form.fields) != 1 ||
		m.form.fields[0].toggleValue() != "15m" {
		t.Fatalf("z did not open the snooze form: %+v", m.form)
	}
	if !m.HandleKey("enter") {
		t.Fatal("submit not consumed")
	}
	items := m.inboxItems()
	var snoozed *inbox.Item
	for i := range items {
		if items[i].Kind == inbox.AgentMessage && !items[i].Snoozed.IsZero() {
			snoozed = &items[i]
		}
	}
	if snoozed == nil {
		t.Fatalf("snooze did not land: %+v", items)
	}
	if !snoozed.Snoozed.Equal(autoClock(2026, 9, 2, 9, 15)) {
		t.Fatalf("expiry = %v, want 09:15", snoozed.Snoozed)
	}
	// Attention count excludes the parked item; the rail shows it dimmed.
	if n := m.AttentionCount(); n != 3 {
		t.Fatalf("attention = %d, want 3 (snoozed excluded)", n)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "snoozed until") || !strings.Contains(out, "09:15") {
		t.Fatalf("snoozed suffix missing:\n%s", out)
	}

	// enter on the parked row refuses the jump by name.
	gotoInbox(m)
	for i := 0; i < 3; i++ {
		m.HandleKey("j")
	}
	if !m.HandleKey("enter") {
		t.Fatal("enter not consumed")
	}
	if !strings.Contains(m.inboxHint, "snoozed until 09:15") ||
		!strings.Contains(m.inboxHint, "z to unsnooze") {
		t.Fatalf("jump refusal hint = %q", m.inboxHint)
	}

	// z again unsnoozes; the row returns at full attention.
	if !m.HandleKey("z") {
		t.Fatal("z (unsnooze) not consumed")
	}
	if n := m.AttentionCount(); n != 4 {
		t.Fatalf("attention after unsnooze = %d, want 4", n)
	}
	// Re-snooze, then u also unparks.
	m.HandleKey("z")
	m.HandleKey("enter")
	m.HandleKey("u")
	if n := m.AttentionCount(); n != 4 {
		t.Fatalf("u did not unsnooze: attention = %d", n)
	}
}

func TestSnoozeRefusesNonMessageKinds(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	gotoInbox(m) // cursor on the approval (rank 0)
	if !m.HandleKey("z") {
		t.Fatal("z not consumed")
	}
	if m.form.kind == fSnooze {
		t.Fatal("approval must not open the snooze form")
	}
	if !strings.Contains(m.inboxHint, "snooze applies to agent messages") {
		t.Fatalf("refusal hint = %q", m.inboxHint)
	}
}

func TestSnoozeTickChain(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	m.now = func() time.Time { return autoClock(2026, 9, 2, 9, 0) }
	gotoInbox(m)
	// No snoozes: nothing arms.
	if m.armSnoozeTick() != nil {
		t.Fatal("chain armed with no snoozes")
	}
	// Snooze the agent message.
	for i := 0; i < 3; i++ {
		m.HandleKey("j")
	}
	m.HandleKey("z")
	m.HandleKey("enter")
	cmd := m.armSnoozeTick()
	if cmd == nil {
		t.Fatal("active snooze did not arm the tick chain")
	}
	// One chain in flight: a second arm is a no-op.
	if m.armSnoozeTick() != nil {
		t.Fatal("double-armed the snooze chain")
	}
	// The tick re-arms while the snooze is still pending.
	if cmd = m.Update(snoozeTickMsg{}); cmd == nil {
		t.Fatal("tick did not re-arm while snooze pending")
	}
	m.snoozeChain = false
	// After unsnoozing the chain stops.
	for i := 0; i < 3; i++ {
		m.HandleKey("j")
	}
	m.HandleKey("u")
	if cmd := m.armSnoozeTick(); cmd != nil {
		t.Fatal("chain armed after the last snooze cleared")
	}
}

func TestInboxSnoozedGolden(t *testing.T) {
	m, _ := newSurface(t)
	seedInbox(t, m)
	m.now = func() time.Time { return autoClock(2026, 9, 2, 9, 0) }
	gotoInbox(m)
	for i := 0; i < 3; i++ {
		m.HandleKey("j")
	}
	m.HandleKey("z")
	m.HandleKey("enter")
	gotoInbox(m)
	golden.Snapshot(t, "workspace_inbox_snoozed", m.View())
}
