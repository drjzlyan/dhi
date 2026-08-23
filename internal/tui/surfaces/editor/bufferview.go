package editor

import (
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

	cmd := e.CommandLine()
	// hide machine-specific absolute paths from the status line
	if p := e.Path(); p != "" && m.openVPath != "" {
		cmd = strings.ReplaceAll(cmd, p, m.openVPath)
	}
	if cmd == "" {
		cmd = theme.Hint().Render("i insert · : cmd · esc tree")
	} else {
		cmd = theme.TabActive().Render(cmd)
	}
	out = append(out, "", cmd)
	return strings.Join(out, "\n")
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

// tabStrip renders the open-buffer tab row.
func tabStrip(bufs []*bufTab, active int) string {
	var parts []string
	for i, t := range bufs {
		label := t.vp
		if j := strings.LastIndex(label, "/"); j >= 0 {
			label = label[j+1:]
		}
		if t.ed.Buffer().Dirty() {
			label += " " + theme.WarningText().Render("●")
		}
		if i == active {
			parts = append(parts, theme.TabActive().Render("["+label+"]"))
		} else {
			parts = append(parts, theme.Hint().Render(" "+label+" "))
		}
	}
	return strings.Join(parts, " ")
}
