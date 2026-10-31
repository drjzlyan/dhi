package editor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/checkpoint"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Undo an agent's chat turn (F-120): the files it changed go back to how
// they were before it ran. Files edited again since are named and kept
// unless the user says to restore them too.

// WithCheckpoints attaches the turn snapshots.
func WithCheckpoints(s *checkpoint.Store) Option { return func(m *Model) { m.cps = s } }

type undoState struct {
	turn      checkpoint.Turn
	changes   []checkpoint.Change
	conflicts map[string]bool // member/path edited after the turn
	err       string
}

func changeKey(c checkpoint.Change) string { return c.Member + "/" + c.Path }

// openUndoTurn prepares the dialog for the newest recorded turn.
func (m *Model) openUndoTurn() string {
	if m.cps == nil {
		return "undo turn: needs git and a workspace"
	}
	turns := m.cps.Turns()
	if len(turns) == 0 {
		return "no agent turn to undo (only chat turns that changed files are kept)"
	}
	t := turns[0]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cs, err := m.cps.Diff(ctx, t.Before, t.After)
	if err != nil {
		return "undo turn: " + err.Error()
	}
	conf, err := m.cps.Conflicts(ctx, t, cs)
	if err != nil {
		return "undo turn: " + err.Error()
	}
	u := &undoState{turn: t, changes: cs, conflicts: map[string]bool{}}
	for _, c := range conf {
		u.conflicts[changeKey(c)] = true
	}
	m.undo = u
	return ""
}

func (m *Model) undoKey(key string) bool {
	u := m.undo
	switch key {
	case "esc", "n":
		m.undo = nil
	case "y", "enter", "f":
		var pick []checkpoint.Change
		for _, c := range u.changes {
			if key == "f" || !u.conflicts[changeKey(c)] {
				pick = append(pick, c)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := m.cps.Restore(ctx, u.turn.Before, pick); err != nil {
			u.err = err.Error()
			return true
		}
		kept := len(u.changes) - len(pick)
		if kept == 0 {
			m.cps.Forget(u.turn.At)
		}
		m.undo = nil
		m.checkDisk() // open buffers follow the restored files
		note := fmt.Sprintf("undid @%s's turn: %s restored", u.turn.Agent, kit.Count(len(pick), "file"))
		if kept > 0 {
			note += fmt.Sprintf(", %s you edited since kept (f restores them too)", kit.Count(kept, "file"))
		}
		m.toast = &kit.Toast{Kind: kit.ToastSuccess, Title: "Turn undone", Detail: note}
	}
	return true
}

func (m *Model) undoView(base string) string {
	u := m.undo
	lines := []string{theme.TextStyle().Render(fmt.Sprintf("@%s's turn %s changed:", u.turn.Agent, ago(u.turn.At)))}
	for i, c := range u.changes {
		if i == 12 {
			lines = append(lines, theme.TextDim().Render(fmt.Sprintf("  … and %d more", len(u.changes)-i)))
			break
		}
		mark := map[string]string{"A": "+ added   ", "M": "~ changed ", "D": "- deleted "}[c.Status]
		row := "  " + mark + c.Member + "/" + c.Path
		if u.conflicts[changeKey(c)] {
			row += "  (you edited it since — kept)"
			lines = append(lines, theme.WarningText().Render(row))
		} else {
			lines = append(lines, theme.TextMuted().Render(row))
		}
	}
	hint := "y restore · esc keep"
	if len(u.conflicts) > 0 {
		hint = "y restore the rest · f restore all, yours too · esc keep"
	}
	lines = append(lines, "", theme.Hint().Render(hint))
	md := kit.Modal{Title: "undo an agent turn", Lines: lines, Error: u.err, Width: 76}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	}
	return t.Format("15:04")
}
