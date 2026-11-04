package editor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/linediff"
	"github.com/drjzlyan/dhi/internal/localhistory"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Local history (F-155): every save DHI makes (:w, autosave, format-on-save
// result, an agent edit saved through the same path) snapshots the saved
// text under .dhi/history. :localhistory lists the open file's snapshots;
// enter compares one with the buffer (read-only), r restores it into the
// buffer as one undo step, not written until :w.

const (
	lhRows    = 12 // rows of the list window
	lhContext = 3  // unchanged lines around a change in the compare view
)

type localHist struct {
	path    string
	name    string
	list    []lhEntry
	sel     int
	compare []string // unified lines of the open comparison; nil in the list
	scroll  int
}

type lhEntry struct {
	localhistory.Entry
	added, removed int // snapshot -> buffer, in lines
}

// histStore is the workspace's snapshot store; nil without a workspace.
func (m *Model) histStore() *localhistory.Store {
	if m.ws == nil {
		return nil
	}
	if m.lhStore == nil {
		m.lhStore = localhistory.New(filepath.Join(m.ws.Root, workspace.DHIDir, "history"))
	}
	return m.lhStore
}

// snapshotSave records text as path's saved content. DHI's own files under
// .dhi (the commit message) are not history.
func (m *Model) snapshotSave(path, text string) {
	st := m.histStore()
	if st == nil || path == "" || within(path, filepath.Join(m.ws.Root, workspace.DHIDir)) || m.editJobTab(path) != nil {
		return // F-165: git's own todo and message files are not the user's history
	}
	st.Snapshot(path, text, m.clock())
}

// openLocalHistory lists the open file's snapshots; it returns a message
// when it cannot.
func (m *Model) openLocalHistory() string {
	e := m.active()
	st := m.histStore()
	if e == nil || e.Path() == "" {
		return "open a file first"
	}
	if st == nil {
		return "local history needs a workspace"
	}
	cur := strings.Split(e.Buffer().Text(), "\n")
	h := &localHist{path: e.Path(), name: filepath.Base(e.Path())}
	for _, en := range st.List(e.Path()) {
		text, err := st.Read(e.Path(), en.ID)
		if err != nil {
			continue
		}
		a, r := lineCounts(strings.Split(text, "\n"), cur)
		h.list = append(h.list, lhEntry{Entry: en, added: a, removed: r})
	}
	if len(h.list) == 0 {
		return h.name + " has no saved snapshots yet"
	}
	m.lh = h
	return ""
}

// lineCounts is how many lines went in and out turning old into cur.
func lineCounts(old, cur []string) (added, removed int) {
	hs, _ := linediff.Hunks(old, cur)
	for _, h := range hs {
		added += len(h.New)
		removed += len(h.Old)
	}
	return added, removed
}

// unifiedLines renders old -> cur as hunk headers, context, and +/- lines.
func unifiedLines(old, cur []string) []string {
	hs, _ := linediff.Hunks(old, cur)
	if len(hs) == 0 {
		return nil
	}
	var out []string
	ctx := func(lines []string) {
		for _, l := range lines {
			out = append(out, " "+l)
		}
	}
	prevEnd := 0
	for i, h := range hs {
		gap := old[prevEnd:h.OldAt]
		if i > 0 && len(gap) <= 2*lhContext {
			ctx(gap)
		} else {
			if i > 0 {
				ctx(gap[:lhContext])
			}
			from := max(h.OldAt-lhContext, prevEnd)
			if i > 0 || from > 0 {
				out = append(out, fmt.Sprintf("@@ line %d @@", from+1))
			}
			ctx(old[from:h.OldAt])
		}
		for _, l := range h.Old {
			out = append(out, "-"+l)
		}
		for _, l := range h.New {
			out = append(out, "+"+l)
		}
		prevEnd = h.OldAt + len(h.Old)
	}
	ctx(old[prevEnd:min(prevEnd+lhContext, len(old))])
	return out
}

func (m *Model) lhKey(key string) bool {
	h := m.lh
	if h.compare != nil {
		switch key {
		case "esc", "q":
			h.compare = nil
		case "j", "down":
			h.scroll = min(h.scroll+1, max(len(h.compare)-lhRows, 0))
		case "k", "up":
			h.scroll = max(h.scroll-1, 0)
		case "pgdown", "ctrl+d":
			h.scroll = min(h.scroll+lhRows, max(len(h.compare)-lhRows, 0))
		case "pgup", "ctrl+u":
			h.scroll = max(h.scroll-lhRows, 0)
		case "g":
			h.scroll = 0
		case "G":
			h.scroll = max(len(h.compare)-lhRows, 0)
		case "r":
			m.lhRestore()
		}
		return true
	}
	switch key {
	case "esc", "q":
		m.lh = nil
	case "j", "down":
		h.sel = min(h.sel+1, len(h.list)-1)
	case "k", "up":
		h.sel = max(h.sel-1, 0)
	case "enter":
		text, err := m.histStore().Read(h.path, h.list[h.sel].ID)
		if err != nil {
			m.lhFail(err.Error())
			return true
		}
		h.compare = unifiedLines(strings.Split(text, "\n"), strings.Split(m.lhBuffer(), "\n"))
		if len(h.compare) == 0 {
			h.compare = []string{"(identical to the buffer)"}
		}
		h.scroll = 0
	case "r":
		m.lhRestore()
	}
	return true
}

func (m *Model) lhBuffer() string {
	if e := m.active(); e != nil && e.Path() == m.lh.path {
		return e.Buffer().Text()
	}
	return ""
}

func (m *Model) lhFail(msg string) {
	if e := m.active(); e != nil {
		e.SetMessage("local history: " + msg)
	}
	m.lh = nil
}

// lhRestore puts the selected snapshot into the buffer as one undo step.
func (m *Model) lhRestore() {
	h := m.lh
	e := m.active()
	if e == nil || e.Path() != h.path {
		m.lhFail("the file's tab is no longer active")
		return
	}
	text, err := m.histStore().Read(h.path, h.list[h.sel].ID)
	if err != nil {
		m.lhFail(err.Error())
		return
	}
	msg := "restored the " + snapWhen(h.list[h.sel].At, m.clock()) + " snapshot — :w to save, u to undo"
	if !e.Buffer().SetText(text) {
		msg = "the snapshot matches the buffer — nothing to restore"
	}
	m.lh = nil
	e.SetMessage(msg)
}

func snapWhen(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

func (m *Model) lhOverlay(base string) string {
	h := m.lh
	var lines []string
	title := "local history · " + h.name
	if h.compare != nil {
		title += " · snapshot → buffer"
		end := min(h.scroll+lhRows, len(h.compare))
		for _, l := range h.compare[h.scroll:end] {
			lines = append(lines, styleUnified(expandTabs(l)))
		}
		lines = append(lines, "", theme.Hint().Render("r restore into the buffer · ↑/↓ scroll · esc back"))
	} else {
		start := max(0, min(h.sel-lhRows+1, len(h.list)-lhRows))
		if h.sel < lhRows {
			start = 0
		}
		now := m.clock()
		for i := start; i < len(h.list) && i < start+lhRows; i++ {
			e := h.list[i]
			label := fmt.Sprintf("%-14s %8s  +%d -%d vs buffer", snapWhen(e.At, now), byteSize(e.Size), e.added, e.removed)
			if i == h.sel {
				lines = append(lines, theme.GlyphCursor+" "+theme.TabActive().Render(label))
			} else {
				lines = append(lines, "  "+theme.TextDim().Render(label))
			}
		}
		lines = append(lines, "", theme.Hint().Render("enter compare · r restore · ↑/↓ move · esc close"))
	}
	md := kit.Modal{Title: title, Lines: lines, Width: min(m.width-4, 100)}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}

// styleUnified colors one line of a unified comparison.
func styleUnified(l string) string {
	switch {
	case strings.HasPrefix(l, "@@"):
		return theme.Hint().Render(l)
	case strings.HasPrefix(l, "+"):
		return theme.AddWash().Render(l)
	case strings.HasPrefix(l, "-"):
		return theme.DelWash().Render(l)
	}
	return theme.TextDim().Render(l)
}

func byteSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
