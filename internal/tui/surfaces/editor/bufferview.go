package editor

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/mattn/go-runewidth"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// cursorStyle inverts the rune under the cursor; it carries no color so
// the theme-only rule stays satisfied.
var cursorStyle = lipgloss.NewStyle().Reverse(true)

func bufferTitle(e *textbuf.Editor) string {
	name := e.Path()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	dot := ""
	if e.Buffer().Dirty() {
		dot = " " + theme.WarningText().Render("●")
	}
	return name + dot + "  " + modePill(e.Mode(), e.VisualBlock())
}

// modePill colors the editing mode (F-064): normal in the editor's
// indigo, insert green, visual amber, command blue.
func modePill(md textbuf.Mode, block bool) string {
	c := theme.Current.Accent2
	switch md {
	case textbuf.ModeInsert:
		c = theme.Current.Success
	case textbuf.ModeVisual:
		c = theme.Current.Warning
	case textbuf.ModeCommand:
		c = theme.Current.Info
	}
	label := md.String()
	if block {
		label = "V-BLOCK" // ctrl+v (F-159)
	}
	return kit.Pill(label, c)
}

// diagChip renders the error/warning count for the active buffer.
func (m *Model) diagChip(e *textbuf.Editor) string {
	errs, warns := m.diagCount(e)
	bulb := ""
	if m.bulbOn && e == m.active() { // F-158: a quick fix is ready on the cursor line
		bulb = theme.WarningText().Render(theme.GlyphBulb)
	}
	if errs == 0 && warns == 0 {
		if bulb == "" {
			return ""
		}
		return "  " + bulb
	}
	var b strings.Builder
	b.WriteString("  ")
	if errs > 0 {
		b.WriteString(theme.DangerText().Render("✗" + itoa(errs)))
	}
	if warns > 0 {
		if errs > 0 {
			b.WriteString(" ")
		}
		b.WriteString(theme.WarningText().Render("⚠" + itoa(warns)))
	}
	if bulb != "" {
		b.WriteString(" " + bulb)
	}
	return b.String()
}

// bufferView renders a scrolled viewport of lines around the cursor with
// a line-number gutter, cursor block, and visual-mode selection.
func (m *Model) bufferView() string {
	e := m.active()
	b := e.Buffer()
	rows := maxInt(m.height-5, 1) // strip + panel padding + command line
	if m.paneH > 0 {
		rows = maxInt(m.paneH-5, 1) // a pane (F-166)
	}

	prefs := m.editorPrefs()
	tabStop = prefs.TabWidth
	if t := m.focusedTab(); t != nil && t.tabWidth > 0 {
		tabStop = min(t.tabWidth, 16) // F-160: .editorconfig beats the setting
	}
	e.SetTabWidth(tabStop) // block columns count tabs like the view (F-159)
	wrap := m.wrapEnabled(prefs)
	curLine := b.Cursor().Line

	top := 0
	if b.LineCount() > rows {
		top = clampIdx(curLine-rows/2, b.LineCount()-rows)
	}
	end := min(top+rows, b.LineCount())
	gut := strconv.Itoa(end)
	if wrap {
		gut = strconv.Itoa(b.LineCount()) // a steady gutter: the window is counted in rows
	}
	gutW := len(gut) + 1
	if !prefs.LineNumbers {
		gutW = 1 // a one-cell column keeps problem colors and git marks
	}
	var wl *wrapLayout
	if wrap {
		wl = m.layoutWrap(e, rows, m.bufTextWidth(gutW))
		top, end = wl.top, wl.end
	}
	// Where the text starts and which line is on top, so a click maps
	// back to a buffer position (F-103); each pane copies it out (F-166).
	m.bufGeom = bufGeom{top: top, rows: end - top, textX: gutW + 1}

	curCol := visCol(b.Line(curLine), b.Cursor().Col)
	var a, z textbuf.Pos
	visual := e.Mode() == textbuf.ModeVisual
	if visual {
		a, z = textbuf.Order(e.VisualStart(), b.Cursor())
		z.Col++             // inclusive of cursor rune like real visual mode
		if e.VisualLine() { // V: whole lines (F-119)
			a.Col, z.Col = 0, len([]rune(b.Line(z.Line)))+1
		}
	}
	hl := e.Highlight()
	// Horizontal scroll (F-119): long lines shift left so the cursor stays
	// in view instead of running off the panel.
	textW := m.bufTextWidth(gutW)
	left := 0
	if !wrap && curCol >= textW-1 { // wrapped lines never scroll sideways
		left = curCol - textW + 2
	}
	m.bufGeom.left = left

	var out []string
	var rowLine, rowStart []int
	curRow, curCell := 0, 0
	path := e.Path()
	syntax := m.syntaxFor(e)
	marks := m.gutterMarks(m.bufs[m.activeTab])
	blame := ""
	if m.blameEnabled(prefs) {
		tab := m.bufs[m.activeTab]
		m.startBlame(tab)
		blame = m.blameText(tab, curLine)
	}
	for l := top; l < end; l++ {
		num := strconv.Itoa(l + 1)
		if !prefs.LineNumbers {
			num = " "
		}
		plain := padLeft(num, gutW-len(num))
		gutter := m.gutterFor(path, l, plain)
		if l == curLine && gutter == theme.Hint().Render(plain) {
			gutter = lipgloss.NewStyle().Foreground(theme.SurfaceAccent()).Bold(true).Render(plain)
		}
		// Tabs render as spaces to the next tab stop (a raw tab would let
		// the terminal jump past the panel edge); columns that index the
		// raw line go through visCol.
		raw := b.Line(l)
		text := expandTabs(raw)

		if bt, bb, bl, br, ok := e.BlockRange(); ok {
			if l >= bt && l <= bb { // ctrl+v: the same display cells on every line (F-159)
				text = markCells(text, bl, br)
			}
		} else if visual {
			selStart, selEnd := 0, len([]rune(text))
			switch {
			case l < a.Line || l > z.Line:
				selStart, selEnd = -1, -1 // untouched line
			case l == a.Line && l == z.Line:
				selStart, selEnd = visCol(raw, min(a.Col, z.Col)), visCol(raw, maxInt(a.Col, z.Col))
			case l == a.Line:
				selStart = visCol(raw, a.Col)
			case l == z.Line:
				selEnd = min(visCol(raw, z.Col), len([]rune(text)))
			}
			text = markRange(text, selStart, selEnd)
		} else if l == curLine {
			text = withCursor(text, curCol)
		} else if hl != "" && strings.Contains(raw, hl) {
			text = markMatches(text, expandTabs(hl)) // plain, so the marks stay exact
		} else if styled, ok := m.searchBufStyled(e, text); ok {
			text = styled // F-168: the search buffer colors its own rows
		} else if st, ok := conflictStyle(e.ConflictRegionAt(l)); ok {
			// Merge-conflict regions (F-026-style washes, F-156) render plain
			// so the background runs unbroken to the edge.
			text = st.Render(padTo(text, textW, wrap))
		} else if syntax != nil {
			// Colorized lines keep the raw text underneath (F-026 P4);
			// cursor and selection lines render plain so the rune-level
			// inversion stays exact.
			if st := syntax.styled(l); st != "" {
				text = expandTabs(st) // escapes never contain a tab
			}
		}
		marker := gitMarkGlyph(marks, l)
		if g, ok := m.debugGutter(path, l); ok {
			marker = g // a breakpoint or the stop line beats a git change mark
		}
		if wrap {
			// One row per segment: the number and marks only on the first.
			segs := wl.segs[l-top]
			rs := []rune(expandTabs(raw))
			for i, st := range segs {
				en := len(rs)
				if i+1 < len(segs) {
					en = segs[i+1]
				}
				row := text
				if len(segs) > 1 {
					c0 := runesWidth(rs[:st])
					row = ansi.Slice(text, c0, c0+runesWidth(rs[st:en])+boolInt(en == len(rs) && l == curLine))
				}
				if blame != "" && l == curLine && i+1 == len(segs) {
					row += blameSuffix(blame, textW-ansi.Width(row)) // last visual row only
				}
				if i == 0 {
					out = append(out, gutter+marker+row)
				} else {
					out = append(out, strings.Repeat(" ", gutW)+row)
				}
				rowLine, rowStart = append(rowLine, l), append(rowStart, st)
				if l == curLine && curCol >= st && (i+1 == len(segs) || curCol < segs[i+1]) {
					curRow = len(out) - 1
					curCell = runesWidth(rs[st:min(curCol, len(rs))])
				}
			}
			continue
		}
		if left > 0 || ansi.Width(text) > textW {
			text = ansi.Slice(text, left, left+textW)
		}
		if blame != "" && l == curLine {
			text += blameSuffix(blame, textW-ansi.Width(text))
		}
		out = append(out, gutter+marker+text)
	}
	if wrap {
		// A cursor line taller than the pane keeps the cursor's row in view.
		if drop := curRow - rows + 1; drop > 0 {
			out, rowLine, rowStart, curRow = out[drop:], rowLine[drop:], rowStart[drop:], curRow-drop
		}
		if len(out) > rows {
			out, rowLine, rowStart = out[:rows], rowLine[:rows], rowStart[:rows]
		}
		if len(out) > 0 {
			m.bufGeom.rowLine, m.bufGeom.rowStart, m.bufGeom.rows = rowLine, rowStart, len(out)
			m.bufGeom.top = rowLine[0]
		}
	}

	if popup := m.popupRows(); len(popup) > 0 && !m.renderingSplit {
		// F-026 P4: completions/code-actions/hover float as a bordered
		// dialog anchored under the cursor cell — not plain rows
		// appended below the buffer.
		x := clampIdx(gutW+curCol, maxInt(m.surfW()-5, 20)-10)
		if wrap { // rows, not lines: the popup hangs under the cursor's row
			x = clampIdx(gutW+curCell, maxInt(m.surfW()-5, 20)-10)
			out = m.floatPopup(out, popup, x, curRow, 0)
		} else {
			out = m.floatPopup(out, popup, x, curLine, top)
		}
	}

	cmd := e.CommandLine()
	// hide machine-specific absolute paths from the status line
	if p := e.Path(); p != "" && m.openVPath != "" {
		cmd = strings.ReplaceAll(cmd, p, m.openVPath)
	}
	switch {
	case m.renameMode:
		cmd = theme.Brand().Render("rename: " + m.renameOld + " → " + string(m.renameInput) + "▌")
	case cmd == "":
		// The keymap lives on the shell status row (ADR-0040); the line
		// shows the cursor line's problem instead (F-116).
		cmd = m.diagLine(e)
	default:
		cmd = theme.TabActive().Render(cmd)
	}
	out = append(out, "", cmd)
	return strings.Join(out, "\n")
}

// popupRows picks the one open LSP popup (completions > code actions >
// hover — the key interceptors make them mutually exclusive in practice).
func (m *Model) popupRows() []string {
	if rows := m.completionView(); len(rows) > 0 {
		return rows
	}
	if rows := m.actionView(); len(rows) > 0 {
		return rows
	}
	if rows := m.hoverView(); len(rows) > 0 {
		return rows
	}
	return m.signatureView()
}

// floatPopup paints a bordered dialog over the buffer rows: x = anchor
// column (below the cursor line), the box covers to the right margin
// so nothing outside it needs cell-level splicing. The box may extend
// past short buffers — the panel pads those rows (clipping at its own
// height budget).
func (m *Model) floatPopup(base []string, rows []string, x, curLine, top int) []string {
	if len(rows) == 0 || len(base) == 0 {
		return base
	}
	avail := maxInt(m.surfW()-5, 20)
	w := 0
	for _, r := range rows {
		if lw := ansi.Width(ansi.Strip(r)); lw > w {
			w = lw
		}
	}
	w = minInt(w+4, maxInt(avail-x, 10))
	box := kit.Modal{Lines: rows, Width: w}
	boxLines := strings.Split(box.View(), "\n")
	y := clampIdx(curLine-top+1, maxInt(len(base)-len(boxLines), 0))
	out := make([]string, len(base), len(base)+len(boxLines))
	copy(out, base)
	pad := strings.Repeat(" ", x)
	for i, bl := range boxLines {
		by := y + i
		if by < len(out) {
			out[by] = ansi.Clip(out[by], x) + bl
			continue
		}
		out = append(out, pad+bl)
	}
	return out
}

// syntaxFor returns the active tab's highlighter, refreshed for the
// current buffer state (F-026 P4; nil-safe).
func (m *Model) syntaxFor(e *textbuf.Editor) *highlighter {
	if m.activeTab >= 0 && m.activeTab < len(m.bufs) {
		t := m.bufs[m.activeTab]
		if t.ed == e && t.syntax != nil {
			t.syntax.refresh(e.Buffer())
			return t.syntax
		}
	}
	return nil
}

// actionView renders the code-action popup rows (same slot as completions).
func (m *Model) actionView() []string {
	if !m.actionOpen || len(m.actionItems) == 0 {
		return nil
	}
	rows := make([]string, 0, min(len(m.actionItems), 8)+1)
	rows = append(rows, theme.Hint().Render("code actions:"))
	end := min(m.actionCur+8, len(m.actionItems))
	start := maxInt(0, end-8)
	for i := start; i < end; i++ {
		label := m.actionItems[i].Title
		if i == m.actionCur {
			rows = append(rows, theme.GlyphCursor+" "+theme.TabActive().Render(label))
		} else {
			rows = append(rows, "  "+theme.TextDim().Render(label))
		}
	}
	return rows
}

// hoverView renders the hover popup above the command line.
func (m *Model) hoverView() []string {
	if !m.hoverOpen || len(m.hoverLines) == 0 {
		return nil
	}
	rows := make([]string, 0, min(len(m.hoverLines), 6)+1)
	rows = append(rows, theme.Hint().Render("hover:"))
	for _, ln := range m.hoverLines[:min(len(m.hoverLines), 6)] {
		rows = append(rows, "  "+theme.TextDim().Render(truncateRunes(ln, maxInt(m.surfW()-6, 20))))
	}
	return rows
}

// withCursor renders col as an inverted block on line.
func withCursor(line string, col int) string {
	r := []rune(line)
	col = clampIdx(col, len(r))
	var cur rune
	if col < len(r) {
		cur = r[col]
	} else {
		cur = ' '
	}
	// At/after the last rune (an empty line, or insert mode past the end)
	// the block is an extra cell and nothing follows it.
	rest := ""
	if col < len(r) {
		rest = string(r[col+1:])
	}
	return string(r[:col]) + cursorStyle.Render(string(cur)) + rest
}

// markRange inverts [start,end) within line; -1,-1 means no selection.
// searchStyle marks matches of the last search (F-119).
var searchStyle = lipgloss.NewStyle().Underline(true).Bold(true)

// markMatches underlines every occurrence of pat in a plain line.
func markMatches(line, pat string) string {
	if pat == "" {
		return line
	}
	var b strings.Builder
	for {
		i := strings.Index(line, pat)
		if i < 0 {
			b.WriteString(line)
			return b.String()
		}
		b.WriteString(line[:i])
		b.WriteString(searchStyle.Foreground(theme.Current.Warning).Render(pat))
		line = line[i+len(pat):]
	}
}

// surfW is the width the buffer being drawn has: its pane's, else the
// surface right of the tree.
func (m *Model) surfW() int {
	if m.paneW > 0 {
		return m.paneW
	}
	return m.width - m.railW()
}

// bufTextWidth is how many cells of text a buffer row shows.
func (m *Model) bufTextWidth(gutW int) int {
	w := m.surfW()
	if m.paneW == 0 && m.chat != nil && m.chat.open {
		w -= chatWidth
	}
	return maxInt(w-2-gutW-1, 10) // panel edges, gutter, change mark
}

func markRange(line string, start, end int) string {
	if start < 0 {
		return line
	}
	r := []rune(line)
	start = clampIdx(start, len(r))
	end = clampIdx(end, len(r))
	if start >= end {
		return line
	}
	return string(r[:start]) + cursorStyle.Render(string(r[start:end])) + string(r[end:])
}

// markCells inverts display cells [start,end) of a plain line; a start past
// the line's end marks nothing (a short line has no part in the block).
func markCells(line string, start, end int) string {
	w := ansi.Width(line)
	end = min(end, w)
	if start >= end {
		return line
	}
	return ansi.Slice(line, 0, start) + cursorStyle.Render(ansi.Slice(line, start, end)) + ansi.Slice(line, end, w)
}

func padLeft(s string, n int) string {
	for i := len(s); i < n; i++ {
		s = " " + s
	}
	return s
}

// tabStrip renders the open-buffer tab row. When tabs exceed avail
// columns the strip keeps the active tab anchored and elides the rest
// with `…+N` / `+N` markers instead of overflowing the panel (F-010:
// many buffers stay on one row).
func tabStrip(bufs []*bufTab, active, avail int) string {
	if avail <= 0 {
		avail = 40
	}
	sep := " "
	render := func(i int) string {
		t := bufs[i]
		label := t.vp
		if j := strings.LastIndex(label, "/"); j >= 0 {
			label = label[j+1:]
		}
		if t.ed.Buffer().Dirty() {
			label += " " + theme.WarningText().Render("●")
		}
		if i == active {
			return kit.Pill(label, theme.SurfaceAccent())
		}
		return theme.Hint().Render(" " + label + " ")
	}

	// Grow a contiguous window around the active tab while it fits.
	l, r := 0, 0
	width := lipgloss.Width(render(active))
	for {
		grew := false
		if li := active - 1 - l; li >= 0 {
			if need := lipgloss.Width(render(li)) + len(sep); width+need <= avail {
				width += need
				l++
				grew = true
			}
		}
		if ri := active + 1 + r; ri < len(bufs) {
			if need := lipgloss.Width(render(ri)) + len(sep); width+need <= avail {
				width += need
				r++
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	// Elide everything outside the window; markers cost columns too, so
	// shrink the window (from the wider outer tab first) until the row
	// fits with the final marker text.
	for {
		omL := active - l
		omR := len(bufs) - 1 - active - r
		var extra int
		var mkL, mkR string
		if omL > 0 {
			mkL = theme.Hint().Render(fmt.Sprintf("…+%d", omL))
			extra += lipgloss.Width(mkL) + len(sep)
		}
		if omR > 0 {
			mkR = theme.Hint().Render(fmt.Sprintf("+%d", omR))
			extra += lipgloss.Width(mkR) + len(sep)
		}
		if width+extra <= avail || (l == 0 && r == 0) {
			var out []string
			if mkL != "" {
				out = append(out, mkL)
			}
			for i := active - l; i <= active+r; i++ {
				out = append(out, render(i))
			}
			if mkR != "" {
				out = append(out, mkR)
			}
			return strings.Join(out, sep)
		}
		// Drop one window tab: prefer the wider outer tab.
		dropR := r > 0 && (l == 0 ||
			lipgloss.Width(render(active+r)) >= lipgloss.Width(render(active-l)))
		if dropR {
			width -= lipgloss.Width(render(active+r)) + len(sep)
			r--
		} else if l > 0 {
			width -= lipgloss.Width(render(active-l)) + len(sep)
			l--
		} else {
			width -= lipgloss.Width(render(active+r)) + len(sep)
			r--
		}
	}
}

// tabStop is the display width of a tab in the buffer view: the
// editor.tab_width setting, applied at each render (F-116).
var tabStop = 4

// expandTabs replaces each tab with spaces up to the next tab stop,
// counting visible cells (escape sequences in styled lines are skipped).
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b { // copy an escape sequence through, width 0
			j := i + 1
			for j < len(s) && (s[j] < 'A' || s[j] > 'Z') && (s[j] < 'a' || s[j] > 'z') {
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\t' {
			n := tabStop - col%tabStop
			b.WriteString(strings.Repeat(" ", n))
			col += n
		} else {
			b.WriteRune(r)
			col += runewidth.RuneWidth(r)
		}
		i += size
	}
	return b.String()
}

// visCol maps a rune column of the raw line to its column in the
// tab-expanded line.
func visCol(raw string, col int) int {
	v := 0
	for i, r := range []rune(raw) {
		if i >= col {
			break
		}
		if r == '\t' {
			v += tabStop - v%tabStop
		} else {
			v++
		}
	}
	if n := len([]rune(raw)); col > n {
		v += col - n // past the end (insert mode after the last rune)
	}
	return v
}

// conflictStyle is the wash for a line inside a merge conflict (F-156).
func conflictStyle(r textbuf.ConflictRegion) (lipgloss.Style, bool) {
	switch r {
	case textbuf.RegionMarker:
		return theme.ConflictMarker(), true
	case textbuf.RegionOurs:
		return theme.ConflictOurs(), true
	case textbuf.RegionBase:
		return theme.ConflictBase(), true
	case textbuf.RegionTheirs:
		return theme.ConflictTheirs(), true
	}
	return lipgloss.Style{}, false
}

// padTo fills a line to the text width so a wash spans the row; wrapped
// lines are left as they are (their rows are cut by cell).
func padTo(s string, w int, wrap bool) string {
	if n := w - ansi.Width(s); !wrap && n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
