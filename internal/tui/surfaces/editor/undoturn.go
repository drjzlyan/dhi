package editor

import (
	"context"
	"fmt"
	"path/filepath"
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
	yours     map[string]bool // member/path you saved while the agent worked
	err       string
	restoring bool // the restore is running off the UI goroutine
}

func changeKey(c checkpoint.Change) string { return c.Member + "/" + c.Path }

// undoReady is the turn's diff and conflicts, read off the UI goroutine.
type undoReady struct {
	turn      checkpoint.Turn
	changes   []checkpoint.Change
	conflicts []checkpoint.Change
	err       error
}

// undoDone is a finished restore.
type undoDone struct {
	turn  checkpoint.Turn
	files int // restored
	kept  int // edited since, left alone
	err   error
}

// openUndoTurn starts preparing the dialog for the newest recorded turn:
// diffing the snapshots runs git over every member, so it happens off the
// UI goroutine and the dialog opens when undoMsgReady lands.
func (m *Model) openUndoTurn() string {
	if m.cps == nil {
		return "undo turn: needs git and a workspace"
	}
	turns := m.cps.Turns()
	if len(turns) == 0 {
		return "no agent turn to undo (only chat turns that changed files are kept)"
	}
	if m.undoLoading {
		return "already checking the last turn…"
	}
	m.undoLoading = true
	t, cps, ch := turns[0], m.cps, m.termMsgs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r := &undoReady{turn: t}
		r.changes, r.err = cps.Diff(ctx, t.Before, t.After)
		if r.err == nil {
			r.conflicts, r.err = cps.Conflicts(ctx, t, r.changes)
		}
		ch <- teaMsg{kind: undoMsgReady, undoReady: r}
	}()
	return "checking what @" + t.Agent + "'s turn changed…"
}

// applyUndoReady opens the dialog for a prepared turn.
func (m *Model) applyUndoReady(r *undoReady) {
	m.undoLoading = false
	if r == nil {
		return
	}
	if r.err != nil {
		m.toast = &kit.Toast{Kind: kit.ToastWarning, Title: "Undo turn", Detail: r.err.Error()}
		return
	}
	t, cs := r.turn, r.changes
	u := &undoState{turn: t, changes: cs, conflicts: map[string]bool{}, yours: map[string]bool{}}
	for _, c := range r.conflicts {
		u.conflicts[changeKey(c)] = true
	}
	// A file you saved during the turn changed between the snapshots too,
	// but not by the agent: keep it unless you say f.
	for _, c := range cs {
		for _, mem := range m.members {
			if mem.name != c.Member {
				continue
			}
			at, ok := m.savedAt[filepath.Join(mem.path, filepath.FromSlash(c.Path))]
			if ok && !at.Before(t.Before.At) && !at.After(t.After.At) {
				u.conflicts[changeKey(c)], u.yours[changeKey(c)] = true, true
			}
		}
	}
	m.undo = u
}

func (m *Model) undoKey(key string) bool {
	u := m.undo
	if u.restoring {
		return true // the files are being written: wait for undoMsgDone
	}
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
		// Restoring rewrites the files through git: off the UI goroutine.
		u.restoring, u.err = true, ""
		t, kept, cps, ch := u.turn, len(u.changes)-len(pick), m.cps, m.termMsgs
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := cps.Restore(ctx, t.Before, pick)
			ch <- teaMsg{kind: undoMsgDone, undoDone: &undoDone{turn: t, files: len(pick), kept: kept, err: err}}
		}()
	}
	return true
}

// applyUndoDone reports a finished restore.
func (m *Model) applyUndoDone(r *undoDone) {
	if r == nil {
		return
	}
	if r.err != nil {
		if m.undo != nil {
			m.undo.restoring, m.undo.err = false, r.err.Error()
		}
		return
	}
	if r.kept == 0 {
		m.cps.Forget(r.turn.At)
	}
	m.undo = nil
	m.checkDisk() // open buffers follow the restored files
	note := fmt.Sprintf("undid @%s's turn: %s restored", r.turn.Agent, kit.Count(r.files, "file"))
	if r.kept > 0 {
		note += fmt.Sprintf(", %s you edited since kept (f restores them too)", kit.Count(r.kept, "file"))
	}
	m.toast = &kit.Toast{Kind: kit.ToastSuccess, Title: "Turn undone", Detail: note}
}

func (m *Model) undoView(base string) string {
	u := m.undo
	lines := []string{theme.TextStyle().Render(fmt.Sprintf("changed while @%s worked (%s):", u.turn.Agent, ago(u.turn.At)))}
	for i, c := range u.changes {
		if i == 12 {
			lines = append(lines, theme.TextDim().Render(fmt.Sprintf("  … and %d more", len(u.changes)-i)))
			break
		}
		mark := map[string]string{"A": "+ added   ", "M": "~ changed ", "D": "- deleted "}[c.Status]
		row := "  " + mark + c.Member + "/" + c.Path
		if u.yours[changeKey(c)] {
			row += "  (you saved it during the turn — kept)"
			lines = append(lines, theme.WarningText().Render(row))
		} else if u.conflicts[changeKey(c)] {
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
	if u.restoring {
		hint = "restoring…"
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
