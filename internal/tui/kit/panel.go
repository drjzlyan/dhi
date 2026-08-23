// Package kit contains DHI's reusable UI primitives. Every component:
//
//   - reads all styling from internal/tui/theme (never raw colors),
//   - renders deterministically for a given state + size,
//   - is testable without a running Bubble Tea program.
package kit

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Panel is a titled, rounded box — the visual unit every surface is built
// from. Focused panels carry the brand accent edge.
//
// Rendering is cell-counted rather than delegated to lipgloss borders so
// widths stay exact under any content and golden snapshots stay stable.
type Panel struct {
	Title   string
	Focused bool
	Width   int // total width including edges; 0 = size to content
	Height  int // total height including edges; 0 = size to content

	content []string
}

// NewPanel returns an empty panel. Chain SetContent to populate.
func NewPanel(title string, focused bool) *Panel {
	return &Panel{Title: title, Focused: focused}
}

// SetContent replaces the panel body; each string is one row (no newlines).
func (p *Panel) SetContent(lines ...string) *Panel { p.content = lines; return p }

// View renders the complete panel including edges and title.
func (p *Panel) View() string {
	edge := theme.PanelEdge(p.Focused)
	titleSt := theme.PanelTitle(p.Focused)
	pad := theme.Current.PadX

	body := p.content
	if len(body) == 0 {
		body = []string{""}
	}

	width := p.Width
	if width == 0 {
		width = maxRuneWidth(body) + pad*2 + 2
	}
	if width < minPanelWidth(p.Title) {
		width = minPanelWidth(p.Title)
	}
	height := p.Height
	if height == 0 {
		height = len(body) + 2
	}

	inner := width - pad*2 - 2

	bg := theme.PanelBg()

	var out []string
	out = append(out, topEdge(width, p.Title, edge, titleSt))

	for y := 0; y < height-2; y++ {
		var row string
		if y < len(body) {
			row = clip(body[y], inner)
		}
		if w := runeWidth(ansi.Strip(row)); w < inner {
			row += strings.Repeat(" ", inner-w)
		}
		line := strings.Repeat(" ", pad) + bg.Render(row) + strings.Repeat(" ", pad)
		out = append(out, edge.Render(lipgloss.RoundedBorder().Left)+line+
			edge.Render(lipgloss.RoundedBorder().Right))
	}

	bottom := lipgloss.RoundedBorder().BottomLeft +
		strings.Repeat(lipgloss.RoundedBorder().Bottom, width-2) +
		lipgloss.RoundedBorder().BottomRight
	out = append(out, edge.Render(bottom))

	return strings.Join(out, "\n")
}

func topEdge(width int, title string, edge, titleSt lipgloss.Style) string {
	rb := lipgloss.RoundedBorder()
	if title == "" {
		return edge.Render(rb.TopLeft + strings.Repeat(rb.Top, width-2) + rb.TopRight)
	}
	head := " " + theme.GlyphChevron + " " + title + " "
	tw := runeWidth(head)
	fill := width - 2 - tw - 2
	if fill < 1 {
		fill = 1
	}
	return edge.Render(rb.TopLeft+rb.Top) +
		titleSt.Render(head) +
		edge.Render(strings.Repeat(rb.Top, fill)+rb.TopRight)
}

func minPanelWidth(title string) int {
	if title == "" {
		return 4
	}
	return runeWidth(" "+theme.GlyphChevron+" "+title+" ") + 5
}

func maxRuneWidth(lines []string) int {
	max := 0
	for _, l := range lines {
		if w := runeWidth(ansi.Strip(l)); w > max {
			max = w
		}
	}
	return max
}

func runeWidth(s string) int {
	n := 0
	for range ansi.Strip(s) {
		n++
	}
	return n
}

// clip cuts s to at most n visible cells, preserving ANSI styling bytes as-is
// when they appear between visible cells.
func clip(s string, n int) string {
	plain := ansi.Strip(s)
	r := []rune(plain)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
