package workspace

import (
	"context"
	"fmt"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/autopilot"
)

// ---- autopilot execution (F-015; ADR-0014 §5) ----
//
// The card UI moved to Settings (F-023); the workspace keeps the
// execution engine: launch catch-up + the in-session tick chain, so
// schedules fire while the user sits in any surface.

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

// autopilotRun posts the card's prompt to the agent's DM channel and
// dispatches a turn through the runtime. A missing/dangling agent or a
// nil runtime refuses with the named fix — never a silent retarget.
func (m *Model) autopilotRun(c autopilot.Card) error {
	if m.rt == nil {
		return fmt.Errorf("autopilot %q: no agent runtime installed", c.Slug)
	}
	if !m.rostered(c.Agent) {
		return fmt.Errorf("autopilot %q: agent %q not on roster (recheck autopilot card or roster)",
			c.Slug, c.Agent)
	}
	msg := bus.Message{
		Channel: "dm:" + c.Agent,
		Author:  bus.Human,
		Text:    "[autopilot " + c.Slug + "] " + c.Prompt,
		At:      m.now(),
	}
	posted := msg
	if m.bus != nil {
		p, err := m.bus.Post(msg)
		if err != nil {
			return fmt.Errorf("autopilot %q: %w", c.Slug, err)
		}
		posted = p
	}
	// Per-run task card (F-015, opt-in): bind a card to this DM message's
	// own thread so recordRun attaches the run record to it.
	if c.CreateTask && m.bus != nil && m.taskStore != nil {
		slug := "auto-" + c.Slug + "-" + fmt.Sprintf("%d", posted.ID)
		if len(slug) > 48 {
			slug = slug[:48]
		}
		if err := m.taskStore.Create(slug, c.Name+" run", c.Agent, ""); err == nil {
			_ = m.taskStore.BindThread(slug, "dm:"+c.Agent, posted.ID)
		}
	}
	go m.rt.Handle(context.Background(), posted)
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
