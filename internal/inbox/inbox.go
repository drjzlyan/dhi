// Package inbox is DHI's pure attention aggregation (F-016, F-017):
// one rail listing every open "needs a human" item across the existing
// seams — pending tool approvals, unaddressed agent messages (DMs and
// @you mentions per the read-mark predicate), failed/timed-out runs on
// open tasks, and in-review tasks. It writes no state: items disappear
// when their source resolves in its home surface.
package inbox

import (
	"fmt"
	"sort"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/sandbox"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/unread"
)

// ItemKind classifies one attention item.
type ItemKind string

// Kinds, in severity order (approval highest).
const (
	Approval     ItemKind = "approval"
	RunFailed    ItemKind = "run_failed"
	InReview     ItemKind = "in_review"
	AgentMessage ItemKind = "agent_message"
)

var rank = map[ItemKind]int{Approval: 0, RunFailed: 1, InReview: 2, AgentMessage: 3}

// Item is one row of the inbox. Row is the display text (label + payload,
// no glyph); the rest is jump payload for the owning surfaces.
type Item struct {
	Kind ItemKind
	Row  string
	At   time.Time // sort root (approval: epoch + pending id)
	// Snoozed is zero unless the item is parked (F-017): it stays in
	// the rail dimmed but leaves the !N count and jump candidates.
	Snoozed time.Time

	ApprovalID int
	Agent      string
	Op         sandbox.Op
	Target     string

	Channel    string
	MsgID      int64
	ThreadRoot int64
	Text       string

	TaskSlug  string
	Assignee  string
	TaskTitle string
	Run       tasks.Run // run_failed jump target (zero value when none)
	ReviewID  string
}

// Build is the pure aggregation: a deterministic, severity-then-age
// ordering over the source seams. Nil/empty sources contribute nothing;
// the inputs are treated as already-stable snapshots. Agent messages
// arrive pre-computed by the read-mark store, whose predicate owns the
// addressed-to-human rule (F-017 §Part B).
func Build(apprs []*tools.Approval, msgs []unread.Item, ts []tasks.Task) []Item {
	var out []Item
	for _, ap := range apprs {
		out = append(out, approvalItem(ap))
	}
	out = append(out, messageItems(msgs)...)
	for _, tk := range ts {
		if r, ok := tk.NewestRun(); ok && !tkDone(tk) && failedRun(r) {
			out = append(out, failedItem(tk, r))
		}
		if tk.Status == tasks.InReview {
			out = append(out, reviewItem(tk))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[out[i].Kind], rank[out[j].Kind]
		if ri != rj {
			return ri < rj
		}
		return out[i].At.Before(out[j].At)
	})
	return out
}

func tkDone(tk tasks.Task) bool { return tk.Status == tasks.Done }

func failedRun(r tasks.Run) bool {
	return r.Status == tasks.RunError || r.Status == tasks.RunTimeout
}

func opWord(op sandbox.Op) string {
	switch op {
	case sandbox.OpWrite:
		return "write"
	case sandbox.OpRead:
		return "read"
	case sandbox.OpExec:
		return "exec"
	case sandbox.OpNet:
		return "net"
	default:
		return string(op)
	}
}

func approvalItem(ap *tools.Approval) Item {
	return Item{
		Kind:       Approval,
		ApprovalID: ap.ID,
		Agent:      ap.Agent,
		Op:         ap.Op,
		Target:     ap.Target,
		At:         time.Unix(0, int64(ap.ID)),
		Row:        "approve  " + ap.Agent + ": " + opWord(ap.Op) + " " + ap.Target,
	}
}

func failedItem(tk tasks.Task, r tasks.Run) Item {
	dur := r.Finished.Sub(r.Started)
	if dur < 0 {
		dur = 0
	}
	label := string(r.Status)
	return Item{
		Kind:      RunFailed,
		TaskSlug:  tk.Slug,
		TaskTitle: tk.Title,
		Assignee:  tk.Assignee,
		Run:       r,
		At:        tk.UpdatedAt,
		Row: fmt.Sprintf("run failed  %s (%s) — %s after %s",
			tk.Slug, r.Agent, label, tasks.DurationText(dur.Milliseconds())),
	}
}

func reviewItem(tk tasks.Task) Item {
	who := tk.Assignee
	if who == "" {
		who = "unassigned"
	}
	rl := tasks.RollupRuns(tk.Runs)
	return Item{
		Kind:      InReview,
		TaskSlug:  tk.Slug,
		TaskTitle: tk.Title,
		Assignee:  tk.Assignee,
		At:        tk.UpdatedAt,
		Row: fmt.Sprintf("in review  %s — %s, %d runs · %s",
			tk.Slug, who, rl.Runs, rl.CostText()),
	}
}

// messageItems lifts the read-mark store's attention set into inbox
// rows (F-017: the F-016 mention rule's successor — DMs and @you
// mentions alike, per unread.AddressedToHuman).
func messageItems(msgs []unread.Item) []Item {
	var out []Item
	for _, um := range msgs {
		out = append(out, Item{
			Kind:       AgentMessage,
			Channel:    um.Msg.Channel,
			MsgID:      um.Msg.ID,
			ThreadRoot: bus.ThreadOf(um.Msg),
			Text:       um.Msg.Text,
			At:         um.Msg.At,
			Snoozed:    um.Snoozed,
			Row:        um.Msg.Channel + "  " + um.Msg.Author + ": \"" + truncQuote(um.Msg.Text) + "\"",
		})
	}
	return out
}

// truncQuote bounds the quoted text so one row stays readable after
// wrap; the trailing ellipsis marks the cut (F-016 row text).
func truncQuote(text string) string {
	const max = 96
	r := []rune(text)
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}
