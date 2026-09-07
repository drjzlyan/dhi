package editor

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

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
	return name + dot + "  " + theme.Brand().Render(e.Mode().String())
}

// diagChip renders the error/warning count for the active buffer.
func (m *Model) diagChip(e *textbuf.Editor) string {
	errs, warns := m.diagCount(e)
	if errs == 0 && warns == 0 {
		return ""
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
	return b.String()
}

// bufferView renders a scrolled viewport of lines around the cursor with
// a line-number gutter, cursor block, and visual-mode selection.
func (m *Model) bufferView() string {
	e := m.active()
	b := e.Buffer()
	rows := maxInt(m.height-5, 1) // strip + panel padding + command line

	top := 0
	if b.LineCount() > rows {
		top = clampIdx(b.Cursor().Line-rows/2, b.LineCount()-rows)
	}
	end := min(top+rows, b.LineCount())

	gut := strconv.Itoa(end)
	gutW := len(gut) + 1

	var a, z textbuf.Pos
	visual := e.Mode() == textbuf.ModeVisual
	if visual {
		a, z = textbuf.Order(e.VisualStart(), b.Cursor())
		z.Col++ // inclusive of cursor rune like real visual mode
	}

	var out []string
	path := e.Path()
	for l := top; l < end; l++ {
		num := strconv.Itoa(l + 1)
		plain := padLeft(num, gutW-len(num))
		gutter := m.gutterFor(path, l, plain)
		text := b.Line(l)

		if visual {
			selStart, selEnd := 0, len([]rune(text))
			switch {
			case l < a.Line || l > z.Line:
				selStart, selEnd = -1, -1 // untouched line
			case l == a.Line && l == z.Line:
				selStart, selEnd = min(a.Col, z.Col), maxInt(a.Col, z.Col)
			case l == a.Line:
				selStart = a.Col
			case l == z.Line:
				selEnd = min(z.Col, len([]rune(text)))
			}
			text = markRange(text, selStart, selEnd)
		} else if l == b.Cursor().Line {
			text = withCursor(text, b.Cursor().Col)
		}
		out = append(out, gutter+" "+text)
	}

	if comp := m.completionView(); len(comp) > 0 {
		out = append(out, "")
		out = append(out, comp...)
	}
	if acts := m.actionView(); len(acts) > 0 {
		out = append(out, "")
		out = append(out, acts...)
	}
	if hov := m.hoverView(); len(hov) > 0 {
		out = append(out, "")
		out = append(out, hov...)
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
		cmd = theme.Hint().Render("i insert · : cmd · esc tree · K/gr/ga lsp")
	default:
		cmd = theme.TabActive().Render(cmd)
	}
	out = append(out, "", cmd)
	return strings.Join(out, "\n")
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
		rows = append(rows, "  "+theme.TextDim().Render(truncateRunes(ln, maxInt(m.width-railWidth-6, 20))))
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
	return string(r[:col]) + cursorStyle.Render(string(cur)) + string(r[col+1:])
}

// markRange inverts [start,end) within line; -1,-1 means no selection.
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
			return theme.TabActive().Render("[" + label + "]")
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
