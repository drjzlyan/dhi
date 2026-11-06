package editor

import (
	"context"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Stash (F-129): z in the git panel lists stashes; n stashes the work tree
// (optional message), a applies, p pops, d drops after a confirm. Runs the
// user's own git (ADR-0044).

type stashView struct {
	list    []gitcore.Stash
	sel     int
	naming  bool      // typing the message for a new stash
	input   kit.Input // the message
	confirm bool      // d pressed: y drops, anything else cancels
	err     string
	busy    string // git is running off the UI goroutine: what it is doing
}

// stashResult is a finished stash command and the fresh list.
type stashResult struct {
	done    string // the panel message on success ("" = a list reload only)
	err     error
	list    []gitcore.Stash
	listErr error
	tree    bool // the work tree changed: refresh what it touches
	close   bool // close the dialog on success
}

// runStash runs op (nil: just list) and lists the stashes, off the UI
// goroutine: stash push, apply and pop rewrite the work tree, seconds on a
// big repo. Keys wait until stashMsgDone lands.
func (m *Model) runStash(busy string, op func(ctx context.Context, r *gitcore.Runner, dir string) error, res stashResult) {
	m.stash.busy = busy
	r, dir, ch := m.gitCLI, m.gitRepo.Path(), m.termMsgs
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if op != nil {
			res.err = op(ctx, r, dir)
		}
		res.list, res.listErr = gitcore.Stashes(ctx, r, dir)
		ch <- teaMsg{kind: stashMsgDone, stashRes: &res}
	}()
}

func (m *Model) openStash() {
	if m.gitRepo == nil || m.gitCLI == nil {
		return
	}
	m.stash = &stashView{input: kit.NewInput("", "message (optional)")}
	m.runStash("reading the stash list…", nil, stashResult{})
}

// applyStash shows a finished stash command.
func (m *Model) applyStash(r *stashResult) {
	s := m.stash
	if r == nil || s == nil {
		return
	}
	s.busy = ""
	if r.listErr == nil {
		s.list = r.list
		s.sel = min(s.sel, max(len(r.list)-1, 0))
	} else {
		s.err = r.listErr.Error()
	}
	if r.err != nil {
		s.err = r.err.Error()
		if r.tree { // conflicts keep the stash (pop) and the tree shows them
			m.stashDone("")
		}
		return
	}
	if r.done == "" {
		return
	}
	s.err = ""
	if r.tree {
		m.stashDone(r.done)
	} else {
		m.gitMessage = r.done
	}
	if r.close {
		m.stash = nil
	}
}

// stashDone refreshes everything a changed work tree touches.
func (m *Model) stashDone(msg string) {
	m.gitMessage = msg
	m.gitErr = ""
	m.invalidateGutters()
	m.invalidateTreeGit()
	m.refreshGitData()
	m.checkDisk() // open buffers follow the files git rewrote
	m.refreshTree()
}

func (m *Model) stashKey(key string) bool {
	s := m.stash
	if s.busy != "" {
		return true // git is running: wait for it
	}
	if s.naming {
		switch key {
		case "esc":
			s.naming = false
		case "enter":
			msg := s.input.Value
			s.naming = false
			s.input = kit.NewInput("", "message (optional)")
			s.sel = 0
			m.runStash("stashing…", func(ctx context.Context, r *gitcore.Runner, dir string) error {
				return gitcore.StashPush(ctx, r, dir, msg)
			}, stashResult{done: "stashed changes", tree: true})
		default:
			s.input.HandleKey(key)
		}
		return true
	}
	if s.confirm {
		s.confirm = false
		if key == "y" && len(s.list) > 0 {
			ref := s.list[s.sel].Ref
			m.runStash("dropping "+ref+"…", func(ctx context.Context, r *gitcore.Runner, dir string) error {
				return gitcore.StashDrop(ctx, r, dir, ref)
			}, stashResult{done: "dropped " + ref})
		}
		return true
	}
	switch key {
	case "esc", "q":
		m.stash = nil
	case "j", "down":
		s.sel = min(s.sel+1, max(len(s.list)-1, 0))
	case "k", "up":
		s.sel = max(s.sel-1, 0)
	case "n":
		s.naming = true
	case "d":
		if len(s.list) > 0 {
			s.confirm = true
		}
	case "a", "p", "enter":
		if len(s.list) == 0 {
			return true
		}
		ref := s.list[s.sel].Ref
		pop := key == "p"
		verb := map[bool]string{true: "popped ", false: "applied "}[pop]
		m.runStash(map[bool]string{true: "popping ", false: "applying "}[pop]+ref+"…",
			func(ctx context.Context, r *gitcore.Runner, dir string) error {
				if pop {
					return gitcore.StashPop(ctx, r, dir, ref)
				}
				return gitcore.StashApply(ctx, r, dir, ref)
			}, stashResult{done: verb + ref, tree: true, close: true})
	}
	return true
}

func (m *Model) stashViewOverlay(base string) string {
	s := m.stash
	var lines []string
	switch {
	case s.busy != "":
		lines = append(lines, theme.TextMuted().Render(s.busy))
	case s.naming:
		lines = append(lines, theme.TextMuted().Render("stash tracked changes"))
		lines = append(lines, s.input.View(56)...)
		lines = append(lines, "", theme.Hint().Render("enter stash · esc back"))
	default:
		if len(s.list) == 0 {
			lines = append(lines, theme.TextDim().Render("no stashes"))
		}
		for i, e := range s.list {
			if i == 12 {
				break
			}
			label := e.Ref + "  " + e.Message + "  · " + e.When
			if i == s.sel {
				lines = append(lines, theme.GlyphCursor+" "+theme.TabActive().Render(label))
			} else {
				lines = append(lines, "  "+theme.TextDim().Render(label))
			}
		}
		keys := "n new · a apply · p pop · d drop · ↑/↓ move · esc close"
		if s.confirm && len(s.list) > 0 {
			keys = "drop " + s.list[s.sel].Ref + "? y drops it for good · any other key cancels"
		}
		lines = append(lines, "", theme.Hint().Render(keys))
	}
	md := kit.Modal{Title: "stash", Lines: lines, Error: strings.TrimSpace(s.err), Width: 76}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}
