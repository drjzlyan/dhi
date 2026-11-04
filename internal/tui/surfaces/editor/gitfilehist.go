package editor

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// File history (F-140): :history, or h in the git panel, lists the commits
// that touched a file (git log --follow); enter opens that commit's patch
// for the file in the read-only hunk viewer. Runs the user's own git (ADR-0044).

const histLimit = 200

type fileHistory struct {
	dir  string // where git runs: the file's folder
	name string // the file's name there
	list []gitcore.FileCommit
	sel  int
	err  string
}

// openHistory lists the history of abs (empty: the open buffer). It returns
// a message when it cannot.
func (m *Model) openHistory(abs string) string {
	if abs == "" {
		e := m.active()
		if e == nil || e.Path() == "" {
			return "open a file first"
		}
		abs = e.Path()
	}
	if m.gitCLI == nil {
		return "git is not available"
	}
	h := &fileHistory{dir: filepath.Dir(abs), name: filepath.Base(abs)}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	list, err := gitcore.FileLog(ctx, m.gitCLI, h.dir, h.name, histLimit)
	if err != nil {
		return "history: " + err.Error()
	}
	if len(list) == 0 {
		return h.name + " has no commits yet"
	}
	h.list = list
	m.hist = h
	return ""
}

// loadCommitDiff fills d with the commit's patch for the file.
func (m *Model) loadCommitDiff(d *diffView) {
	out, err := m.gitRun(d.dir, "show", "--no-color", "--no-ext-diff", "-M", "--format=", d.rev, "--", ":(top)"+d.path)
	if err != nil {
		d.err = err.Error()
		return
	}
	d.head, d.hunks = splitPatch(out)
}

func (m *Model) histKey(key string) bool {
	h := m.hist
	switch key {
	case "esc", "q":
		m.hist = nil
	case "j", "down":
		h.sel = min(h.sel+1, max(len(h.list)-1, 0))
	case "k", "up":
		h.sel = max(h.sel-1, 0)
	case "enter":
		c := h.list[h.sel]
		d := &diffView{path: c.Path, rev: c.Hash, dir: h.dir}
		if d.path == "" {
			d.path = h.name
		}
		m.loadDiff(d)
		m.diff = d
	}
	return true
}

func (m *Model) histOverlay(base string) string {
	h := m.hist
	var lines []string
	// A window of rows that keeps the selection in view.
	const rows = 12
	start := max(0, min(h.sel-rows+1, len(h.list)-rows))
	if h.sel < rows {
		start = 0
	}
	for i := start; i < len(h.list) && i < start+rows; i++ {
		c := h.list[i]
		label := c.Short + "  " + c.When + "  " + c.Author + "  " + c.Subject
		if i == h.sel {
			lines = append(lines, theme.GlyphCursor+" "+theme.TabActive().Render(label))
		} else {
			lines = append(lines, "  "+theme.TextDim().Render(label))
		}
	}
	lines = append(lines, "", theme.Hint().Render("enter diff · ↑/↓ move · esc close"))
	md := kit.Modal{Title: "history · " + h.name, Lines: lines, Error: strings.TrimSpace(h.err), Width: min(m.width-4, 100)}
	return kit.Overlay(strings.Split(base, "\n"), md.View(), m.width, m.height)
}
