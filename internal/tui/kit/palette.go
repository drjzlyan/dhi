package kit

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/fuzzy"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// PaletteItem is one command in a Palette.
type PaletteItem struct {
	Group string // dim prefix ("Go to", "Editor", …); part of the match text
	Title string
	Hint  string // right-aligned dim text (a key or a short note)
	Data  any    // caller payload returned on pick
}

func (it PaletteItem) text() string {
	if it.Group == "" {
		return it.Title
	}
	return it.Group + " " + it.Title
}

// Palette is the fuzzy command launcher (F-041): a query line over a
// ranked list. It owns no actions — the caller maps a picked item's Data
// to behavior — so the same primitive serves the shell and any surface.
type Palette struct {
	Items []PaletteItem
	query []rune
	cur   int
	rank  []int // indexes into Items, best first
}

// paletteRows is how many matches show at once.
const paletteRows = 10

// NewPalette builds a palette over items (input order is the empty-query
// order).
func NewPalette(items []PaletteItem) *Palette {
	p := &Palette{Items: items}
	p.refilter()
	return p
}

func (p *Palette) refilter() {
	texts := make([]string, len(p.Items))
	for i, it := range p.Items {
		texts[i] = it.text()
	}
	p.rank = p.rank[:0]
	for _, r := range fuzzy.Rank(string(p.query), texts) {
		p.rank = append(p.rank, r.Index)
	}
	if p.cur >= len(p.rank) {
		p.cur = max(len(p.rank)-1, 0)
	}
}

// Query returns the current filter text.
func (p *Palette) Query() string { return string(p.query) }

// Matches returns the visible items, best first.
func (p *Palette) Matches() []PaletteItem {
	out := make([]PaletteItem, 0, len(p.rank))
	for _, i := range p.rank {
		out = append(out, p.Items[i])
	}
	return out
}

// HandleKey consumes one key. picked is non-nil when the user chose an
// item; closed reports that the palette should go away (esc or a pick).
func (p *Palette) HandleKey(key string) (picked *PaletteItem, closed bool) {
	switch key {
	case "esc", "ctrl+p":
		return nil, true
	case "enter":
		if p.cur < len(p.rank) {
			it := p.Items[p.rank[p.cur]]
			return &it, true
		}
		return nil, false
	case "down", "ctrl+n", "tab":
		if p.cur < len(p.rank)-1 {
			p.cur++
		}
	case "up", "ctrl+k", "shift+tab":
		if p.cur > 0 {
			p.cur--
		}
	case "backspace":
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.cur = 0
			p.refilter()
		}
	default:
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			p.query = append(p.query, r[0])
			p.cur = 0
			p.refilter()
		}
	}
	return nil, false
}

// View renders the palette as a Modal box of the given total width.
func (p *Palette) View(width int) string {
	if width < 40 {
		width = 40
	}
	inner := width - 4
	lines := []string{theme.Brand().Render("› " + string(p.query) + "▌"), ""}
	if len(p.rank) == 0 {
		lines = append(lines, theme.TextDim().Render("  no matching command"))
	}
	start := 0
	if p.cur >= paletteRows {
		start = p.cur - paletteRows + 1
	}
	for i := start; i < len(p.rank) && i < start+paletteRows; i++ {
		it := p.Items[p.rank[i]]
		left := it.Title
		if it.Group != "" {
			left = theme.TextDim().Render(it.Group+" ") + it.Title
		}
		row := padBetween(left, theme.TextDim().Render(it.Hint), inner-2)
		if i == p.cur {
			// Same cursor glyph as every list; text stays in the same column.
			row = theme.AccentText().Render(theme.GlyphCursor) + theme.Chip().Render(" "+stripToWidth(row, inner-2))
		} else {
			row = "  " + row
		}
		lines = append(lines, row)
	}
	lines = append(lines, "", theme.Hint().Render(fmt.Sprintf("%d commands · enter run · ↑/↓ move · esc close", len(p.rank))))
	m := Modal{Title: "command palette", Lines: lines, Width: width}
	return m.View()
}

// padBetween puts right flush against the right edge of a w-cell row.
func padBetween(left, right string, w int) string {
	gap := w - runeWidth(ansi.Strip(left)) - runeWidth(ansi.Strip(right))
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// stripToWidth drops styling and clips to w cells (used for the selected
// row, which is restyled as a whole).
func stripToWidth(s string, w int) string {
	plain := ansi.Strip(s)
	r := []rune(plain)
	if len(r) > w {
		r = r[:w]
	}
	return string(r)
}
