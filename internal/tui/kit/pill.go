package kit

import (
	"image/color"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Pill renders label as a tinted chip in fg, one space of padding each
// side (F-064). Styled fragments inside label keep the chip background.
func Pill(label string, fg color.Color) string {
	return Restyle(theme.Pill(fg), " "+label+" ")
}

// TabPills renders a tab strip: the active label as a pill in the
// surface accent, the rest as quiet hints padded to the same rhythm.
func TabPills(labels []string, active int) string {
	out := ""
	for i, l := range labels {
		if i == active {
			out += Pill(l, theme.SurfaceAccent())
		} else {
			out += theme.Hint().Render(" " + l + " ")
		}
	}
	return out
}
