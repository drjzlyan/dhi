package workspace

import (
	"context"
	"fmt"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/tasks"
)

// ---- AUTOPILOTS section (F-015) ----

// autoRows lists every autopilot card in store order.
func (m *Model) autoRows() []autopilot.Card {
	if m.autopilots == nil {
		return nil
	}
	return m.autopilots.List()
}

func (m *Model) autopilotsKey(key string) bool {
	rows := m.autoRows()
	c := &m.cursors[secAutopilots]
	clampCursor(c, len(rows))
	sel := func() *autopilot.Card {
		if *c < len(rows) {
			return &rows[*c]
		}
		return nil
	}
	switch key {
	case "j", "down":
		if *c < len(rows)-1 {
			*c++
		}
		return true
	case "k", "up":
		if *c > 0 {
			*c--
		}
		return true
	case "n":
		if m.autopilots == nil {
			return false
		}
		m.form = formState{kind: fAutoNew, fields: []field{
			textField("slug     ", ""),
			textField("name     ", ""),
			textField("agent    ", ""),
			textField("prompt   ", ""),
			textField("schedule ", "daily 09:00"),
		}}
		return true
	case "e":
		if card := sel(); card != nil && m.autopilots != nil {
			if err := m.autopilots.SetEnabled(card.Slug, !card.Enabled); err != nil {
				m.flashErr(err.Error())
			}
			return true
		}
	case "r":
		if card := sel(); card != nil {
			if err := m.runAutopilotNow(card.Slug); err != nil {
				m.flashErr(err.Error())
			}
			return true
		}
	case "x", "d":
		if card := sel(); card != nil {
			m.form = formState{kind: fAutoDeleteConfirm, orig: card.Slug}
			return true
		}
	case "o":
		if card := sel(); card != nil {
			if run, ok := m.newestAgentRun(card.Agent); ok {
				m.replay = openReplay(run)
				m.replay.refresh(m.replayWidth(), m.replayHeight())
			} else {
				m.flashErr("no run records for " + card.Agent)
			}
			return true
		}
	}
	return false
}

// catchUpAutopilots fires every due+enabled card once, in slug order,
// through the ordinary DM turn seam; each success marks ran immediately
// (persist-before-visibility — a crash never double-runs the catch-up).
// Failures flash by name and leave the card due.
func (m *Model) catchUpAutopilots() {
	if m.autopilots == nil {
		return
	}
	now := m.now()
	for _, c := range m.autopilots.Due(now) {
		if err := m.autopilotRun(c); err != nil {
			m.flashErr(err.Error())
			continue
		}
		_ = m.autopilots.MarkRan(c.Slug, now)
	}
}

// runAutopilotNow executes one card immediately, through the same seam
// (F-015 Part C key `r`), and persists the run timestamp on success.
func (m *Model) runAutopilotNow(slug string) error {
	if m.autopilots == nil {
		return fmt.Errorf("autopilot store unavailable")
	}
	c, ok := m.autopilots.Get(slug)
	if !ok {
		return fmt.Errorf("autopilot %q not found", slug)
	}
	if err := m.autopilotRun(c); err != nil {
		return err
	}
	return m.autopilots.MarkRan(slug, m.now())
}

// autopilotRun posts the card's prompt to the agent's DM channel and
// dispatches a turn through the runtime. A missing/dangling agent or a
// nil runtime refuses with the named fix — never a silent retarget.
func (m *Model) autopilotRun(c autopilot.Card) error {
	if m.rt == nil {
		return fmt.Errorf("autopilot %q: no agent runtime installed", c.Slug)
	}
	if !m.rostered(c.Agent) {
		return fmt.Errorf("autopilot %q: agent %q not on roster — add %q under ORG first",
			c.Slug, c.Agent, c.Agent)
	}
	msg := bus.Message{
		Channel: "dm:" + c.Agent,
		Author:  bus.Human,
		Text:    "[autopilot " + c.Slug + "] " + c.Prompt,
		At:      m.now(),
	}
	if m.bus != nil {
		if _, err := m.bus.Post(msg); err != nil {
			return fmt.Errorf("autopilot %q: %w", c.Slug, err)
		}
	}
	go m.rt.Handle(context.Background(), msg)
	return nil
}

// rostered reports whether id is on the active roster.
func (m *Model) rostered(id string) bool {
	if m.roster == nil {
		return false
	}
	for _, a := range m.roster.AgentIDs() {
		if a == id {
			return true
		}
	}
	return false
}

// autoNext renders the rail's "next-due" cell for one card.
func (m *Model) autoNext(c autopilot.Card) string {
	now := m.now()
	if c.Enabled && c.Due(now) {
		return "due"
	}
	t, ok := c.Next(now)
	if !ok {
		return "-"
	}
	switch c.Schedule.Kind {
	case autopilot.KindInterval:
		return "in " + tasks.DurationText(int64(t.Sub(now).Milliseconds()))
	default:
		y, mo, d := now.Date()
		ty, tmo, td := t.Date()
		if y == ty && mo == tmo && d == td {
			return t.Format("15:04")
		}
		return t.Format("Mon 15:04")
	}
}

// autoResult renders the rail's "last-result" cell: the agent's newest
// run outcome when the card has run, else a bare "-".
func (m *Model) autoResult(c autopilot.Card) string {
	if c.LastRun.IsZero() {
		return "-"
	}
	if r, ok := m.newestAgentRun(c.Agent); ok {
		return string(r.Status)
	}
	return "ran " + c.LastRun.Format("15:04")
}
