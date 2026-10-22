package wizard

import (
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// checklistCopy is how each step reads on the start page's checklist
// (F-098): a task-shaped title and what doing it gives you.
var checklistCopy = map[string]struct{ title, why, est string }{
	"workspace":    {"Create a workspace", "group the repos you work on", "10s"},
	"project":      {"Describe the project", "a brief every agent reads first", "1 min"},
	"identity":     {"Git identity", "who your commits are from", "10s"},
	"you":          {"How you like to work", "agents write the way you read", "30s"},
	"instructions": {"House rules", "what every agent always does", "30s"},
	"conventions":  {"Branch and commit style", "names agents follow", "20s"},
	"tools":        {"Your tools", "git, search and language servers DHI uses", "1 min"},
	"cli":          {"Coding agent", "the CLI that does the work", "1 min"},
	"team":         {"Pick a team", "agents that take your tasks", "30s"},
	"integrations": {"Connect services", "GitHub, Linear and more", "2 min"},
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
// that step, esc skips it (or, re-running a done step, leaves it as it
// was — F-108), and neither marks setup finished. onDone runs
// when the dialog closes in this process (not when it relaunches). nil
// when id names no step or the step does not apply.
func Single(env *Env, version string, state setup.State, id string, onDone func()) *Model {
	for _, s := range DefaultSteps(env, version) {
		if s.ID() != id {
			continue
		}
		if !s.Applies() {
			// F-108: a step that is configured no longer applies to a
			// first run, but can still be run again to change it.
			r, ok := s.(rerunnable)
			if !ok || !r.CanRerun() {
				return nil
			}
		}
		m := &Model{env: env, steps: []Step{s}, state: state, skipped: map[string]bool{}}
		m.single, m.onDone = true, onDone
		return m
	}
	return nil
}

// rerunnable is a step that can run again to change what it set up, even
// once Applies says a first run no longer needs it (F-108).
type rerunnable interface{ CanRerun() bool }

// CanRerun: the brief can be edited again (prefilled from the saved one).
func (s *projectStep) CanRerun() bool {
	return s.env.Root != "" && s.env.DetectBrief != nil && s.env.SaveBrief != nil
}

// CanRerun: your profile can be changed again.
func (s *styleStep) CanRerun() bool { return s.env.SaveProfile != nil }

// CanRerun: your instructions can be edited again.
func (s *instructionsStep) CanRerun() bool {
	return s.env.Instructions != nil && s.env.SaveInstructions != nil
}

// CanRerun: another team can be added by role.
func (s *teamStep) CanRerun() bool {
	return s.env.Root != "" && (s.env.ApplyTeam != nil || s.env.ComposeTeam != nil)
}
