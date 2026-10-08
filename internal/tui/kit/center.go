package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// Center places a rendered block roughly centered inside a width×height
// cell area and fills it: the result is exactly height lines, each at
// most width visible cells (wider lines are clipped). A zero width or
// height means "not sized yet" and returns the block unclipped.
func Center(block string, width, height int) string {
	lines := strings.Split(block, "\n")
	maxW := 0
	for _, l := range lines {
		if w := ansi.Width(l); w > maxW {
			maxW = w
		}
	}
	if maxW > width {
		maxW = width
	}
	padX := (width - maxW) / 2
	if padX < 0 {
		padX = 0
	}
	padY := (height - len(lines)) / 2
	if padY < 0 {
		padY = 0
	}
	side := strings.Repeat(" ", padX)
	for i, l := range lines {
		lines[i] = side + l
	}
	top := make([]string, padY)
	out := strings.Join(append(top, lines...), "\n")
	if width <= 0 || height <= 0 {
		return out // unsized (before the first resize): no area to fill
	}
	return Fit(out, width, height)
}
