// INBOX section (F-016): a pure aggregation of every open "needs a
// human" signal — approvals, unreplied @you mentions, failed runs on
// open tasks, in-review tasks. The inbox never resolves anything; it
// launches into the owning surfaces.
package workspace

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/ideation"
	"github.com/drjzlyan/dhi/internal/inbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/unread"
)

// inboxItems aggregates the current attention set. Pure over snapshots:
// each call recomputes, so resolution in a home surface removes a row
// on the next render. Agent messages come from the read-mark store
// (F-017); an unavailable store is a visible hint, never silent.
func (m *Model) inboxItems() []inbox.Item {
	var apprs []*tools.Approval
	if m.approvals != nil {
		apprs = m.approvals.List()
	}
	var msgs []unread.Item
	if m.unreadStore != nil && m.bus != nil {
		msgs = m.unreadStore.Unread(m.bus, m.now())
	}
	var ts []tasks.Task
	if m.taskStore != nil {
		ts = m.taskStore.List()
	}
	var props []ideation.Proposal
	if m.sessions != nil {
		props = m.sessions.PendingProposals()
	}
	items := inbox.Build(apprs, msgs, ts, props)
	for i := range items {
		if items[i].Kind == inbox.InReview {
			if id := m.reviewIDFor(items[i].TaskSlug); id != "" {
				items[i].ReviewID = id
			}
		}
	}
	return items
}

// reviewIDFor finds the review card backing a task's in-review branch,
// if one exists. The reviewer surface consumes plain review ids.
func (m *Model) reviewIDFor(slug string) string {
	if m.reviewSvc == nil {
		return ""
	}
	for _, r := range m.reviewSvc.Store().List() {
		if strings.Contains(r.ID, "task-"+slug) || strings.Contains(r.Target.Head, "task/"+slug) {
			return r.ID
		}
	}
	return ""
}

// inboxKey handles INBOX row navigation, jump keys, and snooze: enter/o
// launches the owning surface; z parks (or unparks) the selected item.
func (m *Model) inboxKey(key string) bool {
	items := m.inboxItems()
	c := &m.cursors[secInbox]
	switch key {
	case "j", "down":
		if *c < len(items)-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "enter", "o":
		if *c < len(items) {
			it := items[*c]
			if !it.Snoozed.IsZero() {
				m.inboxHint = "snoozed until " + snoozeUntilText(it.Snoozed, m.now()) +
					" (z to unsnooze)"
				return true
			}
			m.inboxJump(it)
			return true
		}
	case "a", "x":
		if *c < len(items) && items[*c].Kind == inbox.Dependency {
			m.decideProposal(items[*c], key == "a")
			return true
		}
		return false
	case "z":
		if *c < len(items) {
			it := items[*c]
			switch {
			case it.Kind != inbox.AgentMessage:
				m.inboxHint = "snooze applies to agent messages — " +
					string(it.Kind) + " resolves in its owner surface"
			case !it.Snoozed.IsZero():
				m.unsnoozeSelected(it) // z again = unpark
			default:
				m.snoozeTarget = it
				m.form = openForm(fSnooze, "", toggleField("until ", snoozePresets))
			}
			return true
		}
	case "u":
		if *c < len(items) {
			it := items[*c]
			if !it.Snoozed.IsZero() {
				m.unsnoozeSelected(it)
			}
			return true
		}
	}
	return false
}

// decideProposal accepts (creating a linked task in ToMember) or
// declines a cross-project proposal (F-032). Accepting never weakens
// isolation: the new task is a normal, workflow-bound card.
func (m *Model) decideProposal(it inbox.Item, accept bool) {
	if m.taskStore == nil {
		m.inboxHint = "task store unavailable"
		return
	}
	if !accept {
		if err := m.taskStore.DecidePropagation(it.TaskSlug, it.ToMember, tasks.PropDeclined, ""); err != nil {
			m.inboxHint = err.Error()
			return
		}
		m.inboxHint = "declined cross-project proposal to " + it.ToMember
		return
	}
	slug := "dep-" + it.TaskSlug + "-" + it.ToMember
	if len(slug) > 48 {
		slug = slug[:48]
	}
	title := fmt.Sprintf("Propagate %s change to %s (%s)", it.FromMember, it.ToMember, it.DepKind)
	if err := m.taskStore.Create(slug, title, "", ""); err != nil {
		m.inboxHint = err.Error()
		return
	}
	if err := m.taskStore.DecidePropagation(it.TaskSlug, it.ToMember, tasks.PropAccepted, slug); err != nil {
		m.inboxHint = err.Error()
		return
	}
	m.inboxHint = "created " + slug + " for " + it.ToMember
}

// inboxJump routes one item to its owning surface. A missing seam or
// stale target degrades to a visible named hint — never silent.
func (m *Model) inboxJump(it inbox.Item) {
	m.inboxHint = ""
	switch it.Kind {
	case inbox.Approval:
		if m.openChat != nil && m.openChat() {
			return
		}
		m.inboxHint = "approval jump: editor chat unavailable"
	case inbox.AgentMessage:
		if m.pane != nil && m.pane.openAt(it.Channel, it.ThreadRoot, it.MsgID) {
			m.sec = secChannels
			return
		}
		m.inboxHint = "mention jump: " + it.Channel + " unavailable (channels)"
	case inbox.RunFailed:
		if it.Run.ID == "" {
			m.inboxHint = "run jump: no run recorded for " + it.TaskSlug
			return
		}
		m.replay = openReplay(it.Run)
		m.replay.refresh(m.replayWidth(), m.replayHeight())
		m.sec = secBoard
		// Lane cursors: select the failed card inside its status lane.
		for li, col := range m.boardGroups() {
			for ci, tk := range col {
				if tk.Slug == it.TaskSlug {
					m.boardActive = li
					m.boardCur[li] = ci
				}
			}
		}
	case inbox.InReview:
		if it.ReviewID != "" && m.openReview != nil && m.openReview(it.ReviewID) {
			return
		}
		if it.ReviewID != "" {
			m.inboxHint = "in-review jump: reviewer unavailable (" + it.TaskSlug + ")"
		} else {
			m.inboxHint = "in review  " + it.TaskSlug + " — attach a review first (w)"
		}
	case inbox.Proposal:
		if m.openIdeator != nil && m.openIdeator(it.ProposalID) {
			return
		}
		m.inboxHint = "proposal jump: Ideator unavailable or proposal decided"
	}
}

// AttentionCount feeds the app statusline's !N segment and the INBOX
// rail count: the number of OPEN attention items — snoozed ones are
// parked, not urgent (F-017 §Part D).
func (m *Model) AttentionCount() int {
	n := 0
	for _, it := range m.inboxItems() {
		if it.Snoozed.IsZero() {
			n++
		}
	}
	return n
}

// wireUnreadSeams connects the CHANNELS pane to the read-mark store:
// opening a channel/thread/post advances watermarks; the rail reads
// per-channel counts. Called from New and safe for test-built panes.
func (m *Model) wireUnreadSeams() {
	if m.pane == nil {
		return
	}
	m.pane.onRead = m.markScopeRead
	m.pane.unreadFor = m.unreadForChannel
	m.pane.markAllRead = m.markChannelFullyRead
}

// markChannelFullyRead bulk-marks one channel (top-level + threads) read
// (F-017 bulk action). A missing store degrades by name.
func (m *Model) markChannelFullyRead(channel string) error {
	if m.unreadStore == nil {
		return fmt.Errorf("read marks unavailable")
	}
	return m.unreadStore.MarkChannelRead(channel, m.bus)
}

// markScopeRead advances one watermark scope; a failed write is a named
// degrade (never swallowed, F-011).
func (m *Model) markScopeRead(scope string, upToID int64) {
	if m.unreadStore == nil {
		return
	}
	if err := m.unreadStore.MarkRead(scope, upToID); err != nil {
		m.unreadErr = err.Error()
	}
}

// unreadForChannel reports the cached per-frame count for the rail.
func (m *Model) unreadForChannel(ch string) int {
	if m.unreadCounts == nil {
		return 0
	}
	return m.unreadCounts[ch]
}

// syncUnread refreshes the per-frame rail counts (one Scan per render).
func (m *Model) syncUnread() {
	if m.unreadStore == nil || m.bus == nil {
		m.unreadCounts = nil
		return
	}
	m.unreadCounts = m.unreadStore.Counts(m.bus, m.now())
}

// ---- snooze (F-017 §Part D) ----

// snoozeTickMsg fires every 30s while a snooze is pending so expiries
// flip without interaction (message-driven, deterministic in tests).
type snoozeTickMsg struct{}

const snoozeTickInterval = 30 * time.Second

// armSnoozeTick keeps exactly one expiry chain in flight.
func (m *Model) armSnoozeTick() tea.Cmd {
	if m.snoozeChain || m.unreadStore == nil || !m.unreadStore.HasActiveSnoozes(m.now()) {
		return nil
	}
	m.snoozeChain = true
	return tea.Tick(snoozeTickInterval, func(time.Time) tea.Msg { return snoozeTickMsg{} })
}

func (m *Model) onSnoozeTick() tea.Cmd {
	m.snoozeChain = false
	return m.armSnoozeTick()
}

// snoozeUntilText renders a snooze expiry: "15:04" when it lands today,
// "15:04 Mon 2" once it crosses midnight (F-017 row suffix).
func snoozeUntilText(until, now time.Time) string {
	if until.Day() == now.Day() && until.Month() == now.Month() && until.Year() == now.Year() {
		return until.Format("15:04")
	}
	return until.Format("15:04 Mon 2")
}

// snoozePresets are the "remind me later" choices (F-017 §Part D).
var snoozePresets = []string{"15m", "1h", "4h", "tomorrow 09:00"}

// parseSnoozePreset maps a preset token to an absolute expiry. Pure so
// the form submit is table-tested.
func parseSnoozePreset(preset string, now time.Time) (time.Time, error) {
	switch preset {
	case "15m":
		return now.Add(15 * time.Minute), nil
	case "1h":
		return now.Add(time.Hour), nil
	case "4h":
		return now.Add(4 * time.Hour), nil
	case "tomorrow 09:00":
		next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next, nil
	default:
		return time.Time{}, fmt.Errorf("unknown snooze preset %q (want one of: %s)",
			preset, strings.Join(snoozePresets, ", "))
	}
}

// snoozeSelected parks the selected agent message until the preset
// expiry. Other kinds refuse by name — the snooze schema keys on a bus
// message (channel + message ID), which only agent messages carry.
func (m *Model) snoozeSelected(it inbox.Item, preset string) {
	if m.unreadStore == nil {
		m.inboxHint = "snooze unavailable: read-mark store is down"
		return
	}
	until, err := parseSnoozePreset(preset, m.now())
	if err != nil {
		m.inboxHint = err.Error()
		return
	}
	if err := m.unreadStore.Snooze(it.Channel, it.MsgID, until); err != nil {
		m.inboxHint = "snooze failed: " + err.Error()
		return
	}
	m.inboxHint = ""
}

// unsnoozeSelected lifts the parked state off the selected item.
func (m *Model) unsnoozeSelected(it inbox.Item) {
	if m.unreadStore == nil {
		m.inboxHint = "snooze unavailable: read-mark store is down"
		return
	}
	if err := m.unreadStore.Unsnooze(it.Channel, it.MsgID); err != nil {
		m.inboxHint = "unsnooze failed: " + err.Error()
		return
	}
	m.inboxHint = ""
}
