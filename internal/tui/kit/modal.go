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
type Modal struct {
	Title  string
	Lines  []string // body rows (pre-styled)
	Error  string   // rendered in DangerText under the body when non-empty
	Busy   bool     // renders a busy row and suppresses Error
	Width  int      // total box width incl. edges; 0 = size to content
	Height int      // total box height incl. edges; 0 = content
}

// View renders the box alone (no backdrop).
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
		height = len(body) + 2
	}

	inner := width - pad*2 - 2
	bg := theme.ElevatedBg()
	rb := lipgloss.RoundedBorder()

	out := []string{topEdge(width, m.Title, edge, titleSt)}
	for y := 0; y < height-2; y++ {
		row := ""
		if y < len(body) {
			row = clip(body[y], inner)
		}
		if w := runeWidth(ansi.Strip(row)); w < inner {
			row += strings.Repeat(" ", inner-w)
		}
		line := strings.Repeat(" ", pad) + bg.Render(row) + strings.Repeat(" ", pad)
		out = append(out, edge.Render(rb.Left)+line+edge.Render(rb.Right))
	}
	out = append(out, edge.Render(rb.BottomLeft+
		strings.Repeat(rb.Bottom, width-2)+rb.BottomRight))
	return strings.Join(out, "\n")
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
