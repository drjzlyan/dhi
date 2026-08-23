package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// Center places a rendered block roughly centered inside a width×height
// cell area. Width math uses visible cells only; blocks wider than width
// are clipped per line.
func Center(block string, width, height int) string {
	lines := strings.Split(block, "\n")
	maxW := 0
	for _, l := range lines {
		if w := len([]rune(ansi.Strip(l))); w > maxW {
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
	return strings.Join(append(top, lines...), "\n")
}
