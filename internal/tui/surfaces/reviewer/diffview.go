package reviewer

import (
	"github.com/drjzlyan/dhi/internal/ansi"

	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/gitdiff"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// viewRowKind classifies one logical row of the diff pane.
type viewRowKind uint8

const (
	vrFileHeader viewRowKind = iota
	vrHunkHeader
	vrLineUnified
	vrSideBySide
	vrBinary
)

// viewRow is one logical row. For unified rows Left carries the line;
// side-by-side rows carry both sides (either may be nil).
type viewRow struct {
	kind        viewRowKind
	file        int
	left, right *gitdiff.Line
	text        string // headers / binary notice
}

// diffRows flattens the open diff into logical rows for the active
// layout, cached on a cheap content fingerprint (F-026 P6 — the flatten
// ran per keystroke before).
func (m *Model) diffRows() []viewRow {
	fp := m.diffFingerprint() + " review=" + m.openID
	if fp == m.rowsFP && m.rowsCache != nil {
		return m.rowsCache
	}
	var rows []viewRow
	for fi := range m.files {
		f := &m.files[fi]
		rows = append(rows, viewRow{kind: vrFileHeader, file: fi, text: f.DisplayPath()})
		if f.IsBinary {
			rows = append(rows, viewRow{kind: vrBinary, file: fi,
				text: "binary file — not shown"})
			continue
		}
		for hi := range f.Hunks {
			h := &f.Hunks[hi]
			head := fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s",
				h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Header)
			rows = append(rows, viewRow{kind: vrHunkHeader, file: fi, text: strings.TrimSpace(head)})
			if m.layout == layoutSideBySide {
				for _, pair := range gitdiff.Pair(*h) {
					rows = append(rows, viewRow{kind: vrSideBySide, file: fi,
						left: pair.Left, right: pair.Right})
				}
			} else {
				for li := range h.Lines {
					rows = append(rows, viewRow{kind: vrLineUnified, file: fi, left: &h.Lines[li]})
				}
			}
		}
	}
	m.rowsFP = fp
	m.rowsCache = rows
	return rows
}

// diffFingerprint fingerprints the diff content cheaply (layout + file
// shape) so the flatten cache invalidates without walking hunks.
func (m *Model) diffFingerprint() string {
	var b strings.Builder
	fmt.Fprintf(&b, "layout=%d n=%d", m.layout, len(m.files))
	for fi := range m.files {
		f := &m.files[fi]
		fmt.Fprintf(&b, "|%s:%d", f.DisplayPath(), len(f.Hunks))
		for hi := range f.Hunks {
			fmt.Fprintf(&b, ":%d", len(f.Hunks[hi].Lines))
		}
	}
	return b.String()
}

func (m *Model) rowAt(i int) *viewRow {
	rows := m.diffRows()
	if i >= 0 && i < len(rows) {
		return &rows[i]
	}
	return nil
}

// pathAtRow returns the file path owning row i ("" between files).
func (m *Model) pathAtRow(i int) string {
	rows := m.diffRows()
	path := ""
	for j := 0; j <= i && j < len(rows); j++ {
		if rows[j].kind == vrFileHeader {
			fi := rows[j].file
			if fi < len(m.files) {
				path = m.files[fi].DisplayPath()
			}
		}
	}
	return path
}

// ---- rendering ----

const (
	numCols   = 5 // line-number gutter width per side
	marginCol = 1 // cursor/margin strip before every row
)

// renderDiff paints the viewport: logical rows wrapped to visual lines,
// sliced from scroll, cursor highlighted via the margin strip.
func (m *Model) renderDiff(w, h int, viewed map[string]bool) string {
	rows := m.diffRows()
	if len(rows) == 0 {
		r, ok := m.openReview()
		switch {
		case !ok:
			return theme.TextDim().Render("(no review open — pick one under REVIEWS)")
		case m.busy:
			return theme.TabActive().Render(theme.GlyphBusy + " working…")
		case m.opErr != "":
			return theme.DangerText().Render(m.opErr)
		case r.Done:
			return theme.TextDim().Render("(worktree discarded)")
		default:
			return theme.TextDim().Render("(no diff — hermetic git unavailable?)")
		}
	}

	bodyW := maxInt(w-marginCol, 10)
	sideW := (bodyW - 1) / 2

	type seg struct {
		row int
		txt string
	}
	var segs []seg
	heights := make([]int, len(rows))
	for i, row := range rows {
		var lines []string
		switch row.kind {
		case vrFileHeader:
			lines = []string{m.fileHeaderText(row.file, viewed)}
		case vrHunkHeader:
			lines = []string{m.hunkHeader(row.text, bodyW)}
		case vrBinary:
			lines = []string{theme.TextDim().Render(crop(row.text, bodyW))}
		case vrLineUnified:
			lines = m.unifiedLine(row.left, bodyW)
		case vrSideBySide:
			lines = m.sideBySide(row.left, row.right, sideW)
		}
		heights[i] = len(lines)
		for _, l := range lines {
			segs = append(segs, seg{i, l})
		}
	}

	m.clampScrollHeight(heights, h)
	visible := h
	out := make([]string, 0, visible)
	shown := 0
	skipped := 0
	for _, s := range segs {
		if skipped < m.scroll {
			skipped++
			continue
		}
		if shown >= visible {
			break
		}
		line := s.txt
		if s.row == m.cursor {
			line = theme.GlyphCursor + line
		} else {
			line = " " + line
		}
		out = append(out, line)
		shown++
	}
	for shown < visible { // pad bottom
		out = append(out, "")
		shown++
	}
	return strings.Join(out, "\n")
}

// hunkHeader styles the @@ range (violet) apart from the function
// context (muted) so hunks scan (F-026 P6).
func (m *Model) hunkHeader(text string, w int) string {
	if i := strings.Index(text[3:], "@@ "); i >= 0 {
		split := 3 + i + 2
		rangePart := text[:split]
		ctxPart := text[split:]
		return theme.Accent2Bold().Render(crop(rangePart, w)) +
			theme.TextMuted().Render(crop(ctxPart, maxInt(w-len(rangePart), 0)))
	}
	return theme.Accent2Bold().Render(crop(text, w))
}

func (m *Model) fileHeaderText(fi int, viewed map[string]bool) string {
	f := m.files[fi]
	adds, dels := f.Stat()
	badge := fmt.Sprintf("+%d -%d", adds, dels)
	extra := ""
	if f.IsRename {
		extra = "renamed "
	}
	mark := ""
	if viewed[f.DisplayPath()] {
		mark = " ✓"
	}
	header := theme.TabActive().Render(fmt.Sprintf("%s%s", extra, f.DisplayPath())) +
		theme.TextDim().Render(mark+"  "+badge)
	return header
}

// unifiedLine renders one diff line as [old][new]│text segments;
// added/removed rows carry a background wash and context stays plain
// (readable, F-026 P6).
func (m *Model) unifiedLine(l *gitdiff.Line, w int) []string {
	oldNo, newNo := "", ""
	sign := " "
	var style func() lipgloss.Style
	var wash lipgloss.Style
	switch l.Kind {
	case gitdiff.Add:
		newNo = itoaW(l.NewNo)
		sign = "+"
		style = theme.SuccessText
		wash = theme.AddWash()
	case gitdiff.Del:
		oldNo = itoaW(l.OldNo)
		sign = "-"
		style = theme.DangerText
		wash = theme.DelWash()
	default:
		oldNo = itoaW(l.OldNo)
		newNo = itoaW(l.NewNo)
		style = theme.TextStyle
	}
	gutter := theme.TextDim().Render(padLeft(oldNo, numCols)+" "+
		padLeft(newNo, numCols)+" ") +
		style().Render(sign+" ")
	seg := wrapSegments(gutter, l.Text, style(), w-numCols*2-3)
	if l.Kind != gitdiff.Ctx {
		// Full-row wash (the GitHub look): pad inside the style so the
		// background fills the row; per-line render keeps widths exact.
		for i := range seg {
			seg[i] = wash.Render(padTo(seg[i], w))
		}
	}
	return seg
}

// sideBySide renders old|new halves; missing sides become blank cells.
// Every cell is padded to exactly `half` columns so the divider aligns.
func (m *Model) sideBySide(left, right *gitdiff.Line, half int) []string {
	textW := half - numCols - 1 // room for number+sign before text
	if textW < 4 {
		textW = 4
	}

	cell := func(l *gitdiff.Line) []string {
		if l == nil {
			return []string{strings.Repeat(" ", half)}
		}
		style := theme.TextStyle
		var wash lipgloss.Style
		sign := " "
		switch l.Kind {
		case gitdiff.Add:
			style, wash, sign = theme.SuccessText, theme.AddWash(), "+"
		case gitdiff.Del:
			style, wash, sign = theme.DangerText, theme.DelWash(), "-"
		}
		no := l.NewNo
		if no == 0 {
			no = l.OldNo
		}
		gut := padLeft(itoaW(no), numCols) + sign
		body := wrapPlain(l.Text, textW)
		out := make([]string, 0, len(body))
		for i, b := range body {
			row := ""
			if i == 0 {
				row = style().Render(gut) + style().Render(padTo(b, textW))
			} else {
				row = style().Render(strings.Repeat(" ", numCols+1)) +
					style().Render(padTo(b, textW))
			}
			if l.Kind != gitdiff.Ctx {
				row = wash.Render(padTo(row, half-1))
			}
			out = append(out, row)
		}
		return out
	}

	lLines, rLines := cell(left), cell(right)
	n := maxInt(len(lLines), len(rLines))
	out := make([]string, 0, n)
	div := theme.TextDim().Render("│")
	for i := 0; i < n; i++ {
		lc := strings.Repeat(" ", half)
		if i < len(lLines) {
			lc = padToPlain(lLines[i], half)
		}
		rc := strings.Repeat(" ", half)
		if i < len(rLines) {
			rc = padToPlain(rLines[i], half)
		}
		out = append(out, lc+div+rc)
	}
	return out
}

// padToPlain pads a possibly-styled string to w visible columns.
func padToPlain(s string, w int) string {
	gap := w - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

// clampScrollHeight keeps the cursor's visual block inside the viewport.
func (m *Model) clampScrollHeight(heights []int, h int) {
	total := 0
	for _, x := range heights {
		total += x
	}
	if total <= h {
		m.scroll = 0
		return
	}
	// visual offset of the cursor row
	before := 0
	for i := 0; i < m.cursor && i < len(heights); i++ {
		before += heights[i]
	}
	after := before + heights[minInt(m.cursor, len(heights)-1)]
	if before < m.scroll {
		m.scroll = before
	} else if after > m.scroll+h {
		m.scroll = after - h
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > total-h {
		m.scroll = total - h
	}
}

func (m *Model) clampScroll() {}

// ---- shared text helpers ----

// wrapSegments wraps plain text to width, prefixing only the first
// visual line with gutter.
func wrapSegments(gutter, text string, style lipgloss.Style, w int) []string {
	lines := wrapPlain(text, maxInt(w, 8))
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == 0 {
			out = append(out, gutter+style.Render(l))
		} else {
			out = append(out, strings.Repeat(" ", lipgloss.Width(gutter))+style.Render(l))
		}
	}
	return out
}

func wrapPlain(s string, w int) []string {
	s = strings.ReplaceAll(s, "\t", "    ") // terminals vary; fix at 4
	if s == "" {
		return []string{""}
	}
	r := []rune(s)
	var out []string
	for start := 0; start < len(r); start += w {
		end := minInt(start+w, len(r))
		out = append(out, string(r[start:end]))
	}
	return out
}

func crop(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:maxInt(w-1, 0)]) + "…"
}

func padTo(s string, w int) string {
	gap := w - ansi.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

func padLeft(s string, w int) string {
	if len(s) < w {
		return strings.Repeat(" ", w-len(s)) + s
	}
	return s
}

func itoaW(n int) string {
	if n == 0 {
		return "" // absent-side numbers render blank
	}
	return fmt.Sprintf("%d", n)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
