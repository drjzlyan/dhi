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
	footer  []string
}

// NewPanel returns an empty panel. Chain SetContent to populate.
func NewPanel(title string, focused bool) *Panel {
	return &Panel{Title: title, Focused: focused}
}

// SetContent replaces the panel body; each string is one row (no newlines).
func (p *Panel) SetContent(lines ...string) *Panel { p.content = lines; return p }

// SetFooter adds quiet rows pinned under the body ("… N more", scroll
// cues, position summaries — F-026 P1). The body budget shrinks; at
// tiny heights the footer yields to content.
func (p *Panel) SetFooter(lines ...string) *Panel { p.footer = lines; return p }

// View renders the complete panel including edges and title.
func (p *Panel) View() string {
	edge := theme.PanelEdge(p.Focused)
	titleSt := theme.PanelTitle(p.Focused)
	pad := theme.Current.PadX

	body := p.content
	if len(body) == 0 {
		body = []string{""}
	}
	footerRows := len(p.footer)

	width := p.Width
	if width == 0 {
		width = maxRuneWidth(body) + pad*2 + 2
	}
	if width < minPanelWidth(p.Title) {
		width = minPanelWidth(p.Title)
	}
	height := p.Height
	if height == 0 {
		height = len(body) + footerRows + 2
	}

	inner := width - pad*2 - 2
	budget := height - 2 - footerRows
	if budget < 1 {
		footerRows = 0
		budget = height - 2
	}

	bg := theme.PanelBg()

	var out []string
	out = append(out, topEdge(width, p.Title, edge, titleSt))

	for y := 0; y < budget; y++ {
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
	for y := 0; y < footerRows; y++ {
		row := clip(p.footer[y], inner)
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
	// 2 corner cells + head + fill + TopRight must total exactly width —
	// one short and the top-right corner sits inset from the body (F-024).
	fill := width - 2 - tw - 1
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

// runeWidth measures visible display cells (wide glyphs count twice);
// delegates to ansi.Width, the one width truth (ADR-0015).
func runeWidth(s string) int {
	return ansi.Width(s)
}

// clip cuts s to at most n visible cells, preserving ANSI styling so
// truncated rows keep their colors (F-024).
func clip(s string, n int) string {
	return ansi.Clip(s, n)
}

// ClipEllipsis cuts s to at most n visible cells, appending "…" on the
// cell before the cut so truncation is visible, never silent (F-026).
// n <= 1 returns "" (no room for content + marker).
func ClipEllipsis(s string, n int) string {
	if n <= 1 {
		return ""
	}
	if runeWidth(s) <= n {
		return s
	}
	return ansi.Clip(s, n-1) + "…"
}
