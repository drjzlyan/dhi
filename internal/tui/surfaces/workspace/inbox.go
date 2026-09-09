// INBOX section (F-016): a pure aggregation of every open "needs a
// human" signal — approvals, unreplied @you mentions, failed runs on
// open tasks, in-review tasks. The inbox never resolves anything; it
// launches into the owning surfaces.
package workspace

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/tools"
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
	items := inbox.Build(apprs, msgs, ts)
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

// inboxKey handles INBOX row navigation and jump keys: enter/o launches
// the owning surface for the selected item; nothing else edits.
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
			m.inboxJump(items[*c])
			return true
		}
	}
	return false
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
		m.sec = secTasks
		for i, tk := range m.taskRows() {
			if tk.Slug == it.TaskSlug {
				m.cursors[secTasks] = i
				break
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
	}
}

// AttentionCount feeds the app statusline's !N segment: the number of
// open attention items, recomputed on demand.
func (m *Model) AttentionCount() int {
	return len(m.inboxItems())
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
