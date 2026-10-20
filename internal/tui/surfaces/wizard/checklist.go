package wizard

import (
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// checklistCopy is how each step reads on the start page's checklist
// (F-098): a task-shaped title and what doing it gives you.
var checklistCopy = map[string]struct{ title, why string }{
	"workspace":    {"Create a workspace", "group the repos you work on"},
	"project":      {"Describe the project", "a brief every agent reads first"},
	"identity":     {"Git identity", "who your commits are from"},
	"you":          {"How you like to work", "agents write the way you read"},
	"instructions": {"House rules", "what every agent always does"},
	"conventions":  {"Branch and commit style", "names agents follow"},
	"cli":          {"Coding agent", "the CLI that does the work"},
	"team":         {"Pick a team", "agents that take your tasks"},
	"integrations": {"Connect services", "GitHub, Linear and more"},
}

// Checklist lists the setup steps for the start page and status pill
// (F-098), from per-step records — never from Finished alone. A step is
// listed while it applies, or once it was done or skipped. Migration: a
// finished setup with no record of a step never ran it (a run records
// every step it showed), so the step counts as skipped — settled, no
// nag, reopenable — except the catch-up steps that still apply (today's
// catch-up rule). It is never shown as done.
func Checklist(env *Env, version string, st setup.State) []kit.ChecklistItem {
	var out []kit.ChecklistItem
	for _, s := range DefaultSteps(env, version) {
		id := s.ID()
		c, ok := checklistCopy[id]
		if !ok {
			continue // welcome and done are not tasks
		}
		_, skipped := st.Skipped[id]
		state := kit.ItemPending
		switch {
		case st.Has(id):
			state = kit.ItemDone
		case skipped:
			state = kit.ItemSkipped
		case st.Finished && !catchUpIDs[id]:
			state = kit.ItemSkipped
		}
		if state == kit.ItemPending && !s.Applies() {
			continue
		}
		if state != kit.ItemPending && !st.Settled(id) && !s.Applies() {
			continue // migrated, and no longer relevant here
		}
		out = append(out, kit.ChecklistItem{ID: id, Title: c.title, Why: c.why, State: state})
	}
	return out
}

// ChecklistTitle is a step's checklist title ("" for unknown ids).
func ChecklistTitle(id string) string { return checklistCopy[id].title }

// Single runs one setup step as a focused dialog (F-098): applying marks
// that step, esc skips it, and neither marks setup finished. onDone runs
// when the dialog closes in this process (not when it relaunches). nil
// when id names no step or the step does not apply.
func Single(env *Env, version string, state setup.State, id string, onDone func()) *Model {
	for _, s := range DefaultSteps(env, version) {
		if s.ID() != id {
			continue
		}
		if !s.Applies() {
			return nil
		}
		m := New(env, state, s)
		m.single, m.onDone = true, onDone
		return m
	}
	return nil
}
