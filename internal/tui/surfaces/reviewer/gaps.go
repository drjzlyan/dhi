package reviewer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Unchanged code between hunks is collapsed behind a "⋯ N unchanged lines"
// row that expands from the review worktree's copy of the file (F-049) —
// the "expand context" of a GitHub diff. Only the new side is read, so
// expansion works for PRs, branches and worktrees alike.

// gapKey identifies one collapsed region: file index and its first new line.
type gapKey struct{ fi, from int }

// gapSpan is a collapsed region of unchanged lines [from, to] (new-side
// numbers); offset maps new → old (old = new - offset).
type gapSpan struct {
	key      gapKey
	from, to int
	offset   int
}

// hunkNewRange returns the first and last new-side line a hunk covers; a
// pure-deletion hunk (NewLines == 0) covers no new line.
func hunkNewRange(h gitdiff.Hunk) (first, last int) {
	if h.NewLines == 0 {
		return h.NewStart + 1, h.NewStart
	}
	return h.NewStart, h.NewStart + h.NewLines - 1
}

func hunkOldRange(h gitdiff.Hunk) (first, last int) {
	if h.OldLines == 0 {
		return h.OldStart + 1, h.OldStart
	}
	return h.OldStart, h.OldStart + h.OldLines - 1
}

// sourceLines reads the new-side file of fi from the review worktree
// (cached); nil when it is unavailable (no worktree, deleted, binary,
// unreadable, or too large to be worth expanding).
func (m *Model) sourceLines(fi int) []string {
	if m.srcCache == nil {
		m.srcCache = map[int][]string{}
	}
	if lines, ok := m.srcCache[fi]; ok {
		return lines
	}
	m.srcCache[fi] = nil
	r, ok := m.openReview()
	if !ok || r.WorkRel == "" || r.Done || m.ws == nil || fi < 0 || fi >= len(m.files) {
		return nil
	}
	f := m.files[fi]
	if f.IsBinary || f.IsDeleted {
		return nil
	}
	rel := filepath.Clean(filepath.FromSlash(f.DisplayPath()))
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil // never read outside the review worktree
	}
	data, err := os.ReadFile(filepath.Join(m.ws.Root, filepath.FromSlash(r.WorkRel), rel))
	if err != nil || len(data) > 2<<20 {
		return nil
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	m.srcCache[fi] = lines
	return lines
}

// gaps lists the collapsed regions of file fi in display order: before the
// first hunk, between hunks, and after the last.
func (m *Model) gaps(fi int) []gapSpan {
	src := m.sourceLines(fi)
	f := m.files[fi]
	if src == nil || len(f.Hunks) == 0 {
		return nil
	}
	var out []gapSpan
	add := func(from, to, offset int) {
		if to > len(src) {
			to = len(src)
		}
		if from < 1 {
			from = 1
		}
		if to >= from {
			out = append(out, gapSpan{key: gapKey{fi, from}, from: from, to: to, offset: offset})
		}
	}
	for i, h := range f.Hunks {
		nf, _ := hunkNewRange(h)
		of, _ := hunkOldRange(h)
		if i == 0 {
			add(1, nf-1, nf-of)
		} else {
			_, pl := hunkNewRange(f.Hunks[i-1])
			add(pl+1, nf-1, nf-of)
		}
	}
	last := f.Hunks[len(f.Hunks)-1]
	nl, ol := func() (int, int) { _, a := hunkNewRange(last); _, b := hunkOldRange(last); return a, b }()
	add(nl+1, len(src), nl-ol)
	return out
}

// gapLines materializes an expanded region as context lines (cached so rows
// can point at stable lines).
func (m *Model) gapLines(g gapSpan) []gitdiff.Line {
	if m.gapCache == nil {
		m.gapCache = map[gapKey][]gitdiff.Line{}
	}
	if ls, ok := m.gapCache[g.key]; ok {
		return ls
	}
	src := m.sourceLines(g.key.fi)
	ls := make([]gitdiff.Line, 0, g.to-g.from+1)
	for n := g.from; n <= g.to && n <= len(src); n++ {
		ls = append(ls, gitdiff.Line{Kind: gitdiff.Ctx, NewNo: n, OldNo: n - g.offset, Text: src[n-1]})
	}
	m.gapCache[g.key] = ls
	return ls
}

// expandGap opens one collapsed region.
func (m *Model) expandGap(k gapKey) {
	if m.expanded == nil {
		m.expanded = map[gapKey]bool{}
	}
	m.expanded[k] = true
	m.rowsCache = nil
}

// gapText is the collapsed row's label.
func gapText(g gapSpan) string {
	n := g.to - g.from + 1
	word := "lines"
	if n == 1 {
		word = "line"
	}
	return "⋯ " + itoa(n) + " unchanged " + word + "  ·  e to expand"
}

// gapRow renders the collapsed row.
func (m *Model) gapRow(text string, w int) string {
	return theme.TextMuted().Render(crop(text, w))
}

// resetGapState drops everything derived from the previous diff.
func (m *Model) resetGapState() {
	m.srcCache, m.gapCache, m.expanded = nil, nil, nil
	m.styleCache, m.rowsCache = nil, nil
}
