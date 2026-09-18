package kit

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Modal is the single dialog primitive (F-024): a bordered box with a
// title, optional error and busy rows, stacked over a dimmed backdrop
// via Overlay. Surfaces keep key routing; while a Modal is open they
// must swallow every key (the focus-trap rule) — HandleKey helpers on
// Form encode the canonical esc/enter/tab contract.
//
// F-026 P1: tall bodies scroll (Scrollable + Scroll) with a pinned
// error/busy row and a one-column thumb track; body rows ellipsis-clip
// so truncation is visible; a one-row dim shadow sits under the box.
type Modal struct {
	Title      string
	Lines      []string // body rows (pre-styled)
	Error      string   // rendered in DangerText under the body when non-empty
	Busy       bool     // renders a busy row and suppresses Error
	Width      int      // total box width incl. edges; 0 = size to content
	Height     int      // total box height incl. edges; 0 = content
	Scrollable bool     // window Lines with a thumb track when they overflow

	offset int
}

// Scroll moves the body window by dy rows (negative = up).
func (m *Modal) Scroll(dy int) {
	rows := m.bodyRows()
	if rows <= 0 || len(m.Lines) <= rows {
		m.offset = 0
		return
	}
	m.offset = clamp(m.offset+dy, 0, len(m.Lines)-rows)
}

// ScrollTo moves the body window so row i is visible.
func (m *Modal) ScrollTo(i int) {
	rows := m.bodyRows()
	if rows <= 0 || len(m.Lines) <= rows {
		return
	}
	if i < m.offset {
		m.offset = i
	}
	if i >= m.offset+rows {
		m.offset = i - rows + 1
	}
}

// bodyRows is the scrollable row budget: Height minus top/bottom edges
// and the shadow row.
func (m *Modal) bodyRows() int {
	if m.Height < 4 {
		return 0
	}
	return m.Height - 3
}

// View renders the box alone (no backdrop). Result is exactly
// Width×Height rows when both are set.
func (m *Modal) View() string {
	edge := theme.DialogEdge()
	titleSt := theme.DialogTitle()
	pad := theme.Current.PadX

	body := m.Lines
	if m.Busy {
		body = append(append([]string{}, body...),
			theme.WarningText().Render(theme.GlyphBusy+" working…"))
	} else if m.Error != "" {
		body = append(append([]string{}, body...),
			theme.DangerText().Render(theme.GlyphCross+" "+m.Error))
	}
	if len(body) == 0 {
		body = []string{""}
	}

	width := m.Width
	if width == 0 {
		width = maxRuneWidth(body) + pad*2 + 2
	}
	if width < minPanelWidth(m.Title) {
		width = minPanelWidth(m.Title)
	}
	height := m.Height
	if height == 0 {
		height = len(body) + 3
	}

	inner := width - pad*2 - 2
	bg := theme.ElevatedBg()
	rb := lipgloss.RoundedBorder()

	out := []string{topEdge(width, m.Title, edge, titleSt)}

	// Window the body when scrollable and overflowing; the appended
	// busy/error row stays pinned under the window.
	pinned := 0
	if m.Busy || m.Error != "" {
		pinned = 1
	}
	lines := body[:len(body)-pinned]
	rows := height - 3
	winRows := rows - pinned
	visible := lines
	var track []string
	innerRows := inner
	if m.Scrollable && winRows > 0 && len(lines) > winRows {
		s := Scroller{Total: len(lines), Height: winRows, offset: m.offset}
		start, end := s.Window()
		visible = lines[start:end]
		track = strings.Split(s.Scrollbar(winRows), "\n")
		innerRows = inner - 1
	}
	for y := 0; y < rows; y++ {
		row := ""
		if y < winRows {
			if y < len(visible) {
				row = ellipsisClip(visible[y], innerRows)
			}
			if w := runeWidth(ansi.Strip(row)); w < innerRows {
				row += strings.Repeat(" ", innerRows-w)
			}
			if track != nil {
				row += track[y]
			}
		} else {
			// Pinned busy/error state rows.
			row = ellipsisClip(body[len(body)-pinned+(y-winRows)], inner)
			if w := runeWidth(ansi.Strip(row)); w < inner {
				row += strings.Repeat(" ", inner-w)
			}
		}
		line := strings.Repeat(" ", pad) + bg.Render(row) + strings.Repeat(" ", pad)
		out = append(out, edge.Render(rb.Left)+line+edge.Render(rb.Right))
	}
	out = append(out, edge.Render(rb.BottomLeft+
		strings.Repeat(rb.Bottom, width-2)+rb.BottomRight))

	// Shadow row: the first two cells stay on the backdrop (plain
	// spaces), the rest paint the dim shade so the box reads as raised.
	shadow := theme.OverlayDim().Render(strings.Repeat(" ", width-2))
	out = append(out, "  "+shadow)
	return strings.Join(out[:height], "\n")
}

// Overlay dims backdrop lines and stacks box centered over them. The
// result is exactly width×height rows of width cells.
func Overlay(backdrop []string, box string, width, height int) string {
	// Normalize the backdrop to full-width, veil-painted rows.
	dim := theme.OverlayDim()
	lines := make([]string, 0, height)
	for y := 0; y < height; y++ {
		row := ""
		if y < len(backdrop) {
			row = backdrop[y]
		}
		row = clip(row, width)
		if w := runeWidth(ansi.Strip(row)); w < width {
			row += strings.Repeat(" ", width-w)
		}
		lines = append(lines, dim.Render(row))
	}

	boxLines := strings.Split(box, "\n")
	bw := 0
	for _, l := range boxLines {
		if w := runeWidth(ansi.Strip(l)); w > bw {
			bw = w
		}
	}
	col := (width - bw) / 2
	if col < 0 {
		col = 0
	}
	row0 := (height - len(boxLines)) / 2
	if row0 < 0 {
		row0 = 0
	}
	for i, bl := range boxLines {
		y := row0 + i
		if y >= height {
			break
		}
		line := strings.Repeat(" ", col) + bl
		if w := runeWidth(ansi.Strip(line)); w < width {
			line += dim.Render(strings.Repeat(" ", width-w))
		}
		lines[y] = line
	}
	return strings.Join(lines, "\n")
}
