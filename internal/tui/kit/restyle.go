package kit

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// Restyle wraps s in st so that st survives the resets inside s: a row
// built from styled segments (a red priority glyph, a dim badge) ends
// each segment with a reset that would otherwise drop the row's
// background or selection highlight for the rest of the line. Restyle
// re-opens st after every full or background/foreground reset.
func Restyle(st lipgloss.Style, s string) string {
	open, _, _ := strings.Cut(st.Render("\x00"), "\x00")
	if open == "" {
		return s
	}
	r := strings.NewReplacer(
		"\x1b[0m", "\x1b[0m"+open,
		"\x1b[m", "\x1b[m"+open,
		"\x1b[49m", "\x1b[49m"+open,
		"\x1b[39m", "\x1b[39m"+open,
	)
	return open + r.Replace(s) + "\x1b[0m"
}

// bgOpen is the escape sequence that opens st (used for its background).
func bgOpen(st lipgloss.Style) string {
	open, _, _ := strings.Cut(st.Render("\x00"), "\x00")
	return open
}

// PaintRow clips row to width cells, pads it, and paints every cell on
// bg's background (F-064): nested resets inside row re-open bg, so a
// styled fragment can never leave a terminal-default hole behind it,
// and nothing is painted past width.
func PaintRow(row string, width int, bg lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	row = clip(row, width)
	if w := runeWidth(row); w < width {
		row += strings.Repeat(" ", width-w)
	}
	return ansi.Fill(row, bgOpen(bg))
}
