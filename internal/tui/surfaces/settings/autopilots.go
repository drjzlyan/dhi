package settings

import (
	"context"
	"fmt"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/tasks"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- AUTOPILOTS section (F-023): CRUD + run-now ----
//
// Execution stays wired to the workspace surface (ADR-0014 §5): launch
// catch-up and the tick chain live there. This section edits cards and
// triggers explicit runs through the same seam.

func (m *Model) autoRows() []autopilot.Card {
	if m.d.Autopilots == nil {
		return nil
	}
	return m.d.Autopilots.List()
}

func (m *Model) autopilotsKey(key string) bool {
	rows := m.autoRows()
	switch key {
	case "j", "down":
		if m.autoCur < len(rows)-1 {
			m.autoCur++
			m.flash = ""
		}
		return true
	case "k", "up":
		if m.autoCur > 0 {
			m.autoCur--
			m.flash = ""
		}
		return true
	case "n":
		if m.d.Autopilots == nil {
			return false
		}
		m.openFormDialog("new autopilot", dlgAutoNew, "",
			kit.NewTextField("slug", ""),
			kit.NewTextField("name", ""),
			kit.NewTextField("agent", ""),
			kit.NewTextField("prompt", ""),
			kit.NewTextField("schedule", "daily 09:00"))
		return true
	case "e":
		if m.d.Autopilots == nil || m.autoCur >= len(rows) {
			return false
		}
		card := rows[m.autoCur]
		if err := m.d.Autopilots.SetEnabled(card.Slug, !card.Enabled); err != nil {
			m.flash = "failed: " + err.Error()
			return true
		}
		if card.Enabled {
			m.flash = card.Slug + " paused"
		} else {
			m.flash = card.Slug + " armed"
		}
		return true
	case "r":
		if m.autoCur < len(rows) {
			if err := m.runAutopilotNow(rows[m.autoCur].Slug); err != nil {
				m.flash = "failed: " + err.Error()
				return true
			}
			m.flash = rows[m.autoCur].Slug + " dispatched"
			return true
		}
	case "x", "d":
		if m.autoCur < len(rows) {
			m.openConfirmDialog("remove autopilot",
				"remove autopilot "+rows[m.autoCur].Slug+"?",
				rows[m.autoCur].Slug, dlgAutoDelete)
			return true
		}
	case "o":
		if m.autoCur < len(rows) {
			m.showLastRun(rows[m.autoCur])
			return true
		}
	}
	return false
}

func (m *Model) submitAutoNew() {
	if m.d.Autopilots == nil {
		m.dform.SetError("autopilot store unavailable")
		return
	}
	vals := m.dform.Values()
	slug := vals[0]
	sch, err := autopilot.ParseSchedule(vals[4])
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	if _, err := m.d.Autopilots.Create(slug, vals[1], vals[2], vals[3], sch); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.flash = "autopilot " + slug + " created"
}

// runAutopilotNow executes one card immediately through the same seam
// as the workspace (post to the DM, dispatch, success-only MarkRan).
func (m *Model) runAutopilotNow(slug string) error {
	if m.d.Autopilots == nil {
		return fmt.Errorf("autopilot store unavailable")
	}
	c, ok := m.d.Autopilots.Get(slug)
	if !ok {
		return fmt.Errorf("autopilot %q not found", slug)
	}
	if m.d.Runtime == nil {
		return fmt.Errorf("autopilot %q: no agent runtime installed", slug)
	}
	ok, err := m.agentOnRoster(c.Agent)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("autopilot %q: agent %q not on roster (recheck autopilot card or roster)",
			c.Slug, c.Agent)
	}
	msg := bus.Message{
		Channel: "dm:" + c.Agent,
		Author:  bus.Human,
		Text:    "[autopilot " + c.Slug + "] " + c.Prompt,
		At:      m.now(),
	}
	if m.d.Bus != nil {
		if _, err := m.d.Bus.Post(msg); err != nil {
			return fmt.Errorf("autopilot %q: %w", c.Slug, err)
		}
	}
	go m.d.Runtime.Handle(context.Background(), msg)
	return m.d.Autopilots.MarkRan(c.Slug, m.now())
}

// showLastRun renders the newest recorded run for the card's agent in
// a read-only modal (the full replay pane stays on the board, F-021).
func (m *Model) showLastRun(card autopilot.Card) {
	if m.d.Tasks == nil {
		m.flash = "task store unavailable — no run history"
		return
	}
	run, ok := newestAgentRun(m.d.Tasks, card.Agent)
	if !ok {
		m.flash = "no run records for " + card.Agent
		return
	}
	lines := []string{
		"status  " + string(run.Status),
		"runtime " + run.Runtime,
		"finished " + run.Finished.Format("15:04"),
	}
	if run.Error != "" {
		lines = append(lines, theme.DangerText().Render("error   "+run.Error))
	}
	if run.Summary != "" {
		s := run.Summary
		if len(s) > 200 {
			s = s[:197] + "…"
		}
		lines = append(lines, "", s)
	}
	lines = append(lines, "", theme.TextMuted().Render(run.Transcript))
	m.openDisplayDialog("last run — "+card.Slug, lines)
}

// newestAgentRun mirrors the workspace's routing helper: the most
// recently finished run recorded by id across all cards.
func newestAgentRun(store *tasks.Store, id string) (tasks.Run, bool) {
	var best tasks.Run
	ok := false
	for _, tk := range store.List() {
		for _, r := range tk.Runs {
			if r.Agent != id {
				continue
			}
			if !ok || r.Finished.After(best.Finished) {
				best, ok = r, true
			}
		}
	}
	return best, ok
}

func (m *Model) autopilotsView() []string {
	if m.d.Autopilots == nil {
		return []string{theme.TextDim().Render(
			"(autopilot store unavailable — not inside a workspace)")}
	}
	rows := m.autoRows()
	out := []string{}
	if w := m.d.Autopilots.Warnings(); len(w) > 0 {
		out = append(out, theme.DangerText().Render(
			fmt.Sprintf("%d malformed card(s) skipped", len(w))))
	}
	if len(rows) == 0 {
		out = append(out, theme.TextDim().Render(
			"(no autopilots — \"n\" to schedule one)"))
		return out
	}
	out = append(out, theme.TextMuted().Render(
		"  "+padTo("name", 20)+padTo("agent", 12)+padTo("schedule", 28)+
			padTo("next-due", 16)+"last-result"))
	for i, card := range rows {
		style := theme.TextDim()
		if i == m.autoCur {
			style = theme.TabActive()
		}
		sched := card.Schedule.String()
		if !card.Enabled {
			sched = "paused " + sched
		}
		line := cursorGlyph(i == m.autoCur) +
			style.Render(padTo(card.Name, 20)) +
			theme.Hint().Render(padTo(card.Agent, 12)) +
			style.Render(padTo(sched, 28)) +
			theme.Hint().Render(padTo(m.autoNext(card), 16)) +
			theme.TextDim().Render(m.autoResult(card))
		out = append(out, line)
	}
	return out
}

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
		return "soon"
	default:
		y, mo, d := now.Date()
		ty, tmo, td := t.Date()
		if y == ty && mo == tmo && d == td {
			return t.Format("15:04")
		}
		return t.Format("Mon 15:04")
	}
}

func (m *Model) autoResult(c autopilot.Card) string {
	if c.LastRun.IsZero() {
		return "-"
	}
	if m.d.Tasks != nil {
		if r, ok := newestAgentRun(m.d.Tasks, c.Agent); ok {
			return string(r.Status)
		}
	}
	return "ran " + c.LastRun.Format("15:04")
}

func cursorGlyph(on bool) string {
	if on {
		return theme.GlyphCursor + " "
	}
	return "  "
}
