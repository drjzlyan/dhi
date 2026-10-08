package editor

import (
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/linediff"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Git change markers in the editor gutter (F-040): lines added, modified
// or followed by a deletion relative to HEAD. Markers live in the column
// between the line number and the text, so the layout does not move.

type gutterState struct {
	loaded bool
	base   []string // HEAD lines; valid when ok
	ok     bool     // file tracked at HEAD (untracked files get no markers)

	marks    []linediff.Mark
	marksSeq uint64
	marksSet bool
}

// repoRel finds the member repo that holds abs and the repo-relative path.
func (m *Model) repoRel(abs string) (root, rel string, ok bool) {
	for _, mem := range m.members {
		if r, err := filepath.Rel(mem.path, abs); err == nil && !strings.HasPrefix(r, "..") {
			return mem.path, r, true
		}
	}
	return "", "", false
}

// loadGutterBase reads the file's HEAD content once per tab (reset by
// invalidateGutters when HEAD moves).
func (m *Model) loadGutterBase(t *bufTab) {
	g := &t.gutter
	g.loaded = true
	g.ok = false
	root, rel, found := m.repoRel(t.path)
	if !found || !gitcore.IsRepo(root) {
		return
	}
	rp, err := gitcore.Open(root)
	if err != nil {
		return
	}
	content, tracked, err := rp.HeadContent(rel)
	if err != nil || !tracked {
		return
	}
	g.base, g.ok = strings.Split(content, "\n"), true
}

// gutterMarks returns the per-line marks for t, recomputed only when the
// buffer changed since the last render. nil means "no markers".
func (m *Model) gutterMarks(t *bufTab) []linediff.Mark {
	g := &t.gutter
	if !g.loaded {
		m.loadGutterBase(t)
	}
	if !g.ok {
		return nil
	}
	b := t.ed.Buffer()
	if g.marksSet && g.marksSeq == b.Seq() {
		return g.marks
	}
	marks, ok := linediff.Diff(g.base, b.Lines())
	if !ok {
		marks = nil
	}
	g.marks, g.marksSeq, g.marksSet = marks, b.Seq(), true
	return g.marks
}

// invalidateGutters drops every tab's HEAD snapshot (after a commit).
func (m *Model) invalidateGutters() {
	for _, t := range m.bufs {
		t.gutter = gutterState{}
	}
}

// gitMarkGlyph renders the marker column for line l (a space when the
// line is unchanged or markers are unavailable).
func gitMarkGlyph(marks []linediff.Mark, l int) string {
	if l < 0 || l >= len(marks) {
		return " "
	}
	switch marks[l] {
	case linediff.Added:
		return theme.SuccessText().Render(theme.GlyphGutterBar)
	case linediff.Modified:
		return theme.WarningText().Render(theme.GlyphGutterBar)
	case linediff.DeletedBelow:
		return theme.DangerText().Render(theme.GlyphGutterDel)
	}
	return " "
}
