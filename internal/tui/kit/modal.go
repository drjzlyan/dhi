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
// so truncation is visible. F-064: the box is one solid elevated block
// (edges included) and Overlay touches no cell outside it — no veil, no
// shadow.
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

// bodyRows is the scrollable row budget: Height minus top/bottom edges.
func (m *Modal) bodyRows() int {
	if m.Height < 3 {
		return 0
	}
	return m.Height - 2
}

// View renders the box alone (no backdrop). Result is exactly
// Width×Height rows when both are set.
func (m *Modal) View() string {
	bg := theme.ElevatedBg()
	edge := theme.DialogEdge().Background(theme.Current.BgElevated)
	titleSt := theme.DialogTitle().Background(theme.Current.BgElevated)
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
		height = len(body) + 2
	}

	inner := width - pad*2 - 2
	rb := lipgloss.RoundedBorder()

	out := []string{topEdge(width, m.Title, edge, titleSt)}

	// Window the body when scrollable and overflowing; the appended
	// busy/error row stays pinned under the window.
	pinned := 0
	if m.Busy || m.Error != "" {
		pinned = 1
	}
	lines := body[:len(body)-pinned]
	rows := height - 2
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
				row = ClipEllipsis(visible[y], innerRows)
			}
			if track != nil {
				row = PaintRow(row, innerRows, bg) + track[y]
			}
		} else {
			// Pinned busy/error state rows.
			row = ClipEllipsis(body[len(body)-pinned+(y-winRows)], inner)
		}
		line := PaintRow(strings.Repeat(" ", pad)+row, inner+pad*2, bg)
		out = append(out, edge.Render(rb.Left)+line+edge.Render(rb.Right))
	}
	out = append(out, edge.Render(rb.BottomLeft+
		strings.Repeat(rb.Bottom, width-2)+rb.BottomRight))
	return strings.Join(out[:height], "\n")
}

// Overlay stacks box centered over the backdrop and changes nothing
// else (F-064): backdrop rows keep their own colors, and each box row is
// spliced in with the backdrop's cells on either side intact, so pane
// borders and neighbouring content survive. The result is exactly
// width×height rows of width cells.
func Overlay(backdrop []string, box string, width, height int) string {
	lines := make([]string, 0, height)
	for y := 0; y < height; y++ {
		row := ""
		if y < len(backdrop) {
			row = backdrop[y]
		}
		row = clip(row, width)
		if w := runeWidth(row); w < width {
			row += strings.Repeat(" ", width-w)
		}
		lines = append(lines, row)
	}

	boxLines := strings.Split(box, "\n")
	bw := 0
	for _, l := range boxLines {
		if w := runeWidth(l); w > bw {
			bw = w
		}
	}
	if bw > width {
		bw = width
	}
	col := (width - bw) / 2
	row0 := (height - len(boxLines)) / 2
	if row0 < 0 {
		row0 = 0
	}
	// A one-cell margin beside the box is cleared of backdrop text (its
	// background and any border glyphs stay) so words never butt
	// against the box edge.
	// The margin widens to whole phrases, so no fragment of a word or a
	// card title is left peeking out beside the box.
	span := func(back string) (lo, hi int) {
		cells := ansi.Cells(back)
		word := func(x int) bool {
			if x < 0 || x >= len(cells) {
				return false
			}
			r := cells[x].R
			return r != ' ' && (r < 0x2500 || r > 0x259F)
		}
		// A phrase continues across single spaces ("▽ CSV export"), so
		// the clear stops only at a gap of two or more cells or a border.
		lo, hi = max(col-1, 0), min(col+bw+1, width)
		for word(lo-1) || (lo-1 >= 0 && cells[lo-1].R == ' ' && word(lo-2)) {
			lo--
		}
		for word(hi) || (hi < len(cells) && cells[hi].R == ' ' && word(hi+1)) {
			hi++
		}
		return lo, hi
	}
	for i, bl := range boxLines {
		y := row0 + i
		if y >= height {
			break
		}
		bl = clip(bl, bw)
		if w := runeWidth(bl); w < bw {
			bl += strings.Repeat(" ", bw-w)
		}
		back := lines[y]
		lo, hi := span(back)
		lines[y] = ansi.Clip(back, lo) + "\x1b[0m" +
			ansi.Blank(ansi.Slice(back, lo, col)) + bl + "\x1b[0m" +
			ansi.Blank(ansi.Slice(back, col+bw, hi)) + ansi.Slice(back, hi, width)
	}
	return strings.Join(lines, "\n")
}
