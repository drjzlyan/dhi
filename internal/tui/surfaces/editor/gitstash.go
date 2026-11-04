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
}

func (m *Model) stashCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

func (m *Model) openStash() {
	if m.gitRepo == nil || m.gitCLI == nil {
		return
	}
	s := &stashView{input: kit.NewInput("", "message (optional)")}
	m.stash = s
	m.reloadStash()
}

func (m *Model) reloadStash() {
	s := m.stash
	ctx, cancel := m.stashCtx()
	defer cancel()
	list, err := gitcore.Stashes(ctx, m.gitCLI, m.gitRepo.Path())
	if err != nil {
		s.err = err.Error()
		return
	}
	s.list = list
	s.sel = min(s.sel, max(len(list)-1, 0))
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
	if s.naming {
		switch key {
		case "esc":
			s.naming = false
		case "enter":
			ctx, cancel := m.stashCtx()
			defer cancel()
			if err := gitcore.StashPush(ctx, m.gitCLI, m.gitRepo.Path(), s.input.Value); err != nil {
				s.err = err.Error()
				s.naming = false
				return true
			}
			s.naming, s.err = false, ""
			s.input = kit.NewInput("", "message (optional)")
			m.stashDone("stashed changes")
			s.sel = 0
			m.reloadStash()
		default:
			s.input.HandleKey(key)
		}
		return true
	}
	if s.confirm {
		s.confirm = false
		if key == "y" && len(s.list) > 0 {
			ref := s.list[s.sel].Ref
			ctx, cancel := m.stashCtx()
			defer cancel()
			if err := gitcore.StashDrop(ctx, m.gitCLI, m.gitRepo.Path(), ref); err != nil {
				s.err = err.Error()
			} else {
				s.err = ""
				m.gitMessage = "dropped " + ref
				m.reloadStash()
			}
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
		ctx, cancel := m.stashCtx()
		defer cancel()
		var err error
		if pop {
			err = gitcore.StashPop(ctx, m.gitCLI, m.gitRepo.Path(), ref)
		} else {
			err = gitcore.StashApply(ctx, m.gitCLI, m.gitRepo.Path(), ref)
		}
		if err != nil { // conflicts keep the stash (pop) and the tree shows them
			s.err = err.Error()
			m.stashDone("")
			m.reloadStash()
			return true
		}
		verb := "applied "
		if pop {
			verb = "popped "
		}
		m.stash = nil
		m.stashDone(verb + ref)
	}
	return true
}

func (m *Model) stashViewOverlay(base string) string {
	s := m.stash
	var lines []string
	switch {
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
