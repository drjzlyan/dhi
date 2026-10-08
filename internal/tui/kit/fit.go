package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
)

// Fit forces block to exactly h lines, none wider than w visible cells
// (F-054 layout contract). Short blocks are padded with blank lines,
// tall ones clipped at the bottom, wide lines clipped with their styling
// closed so a cut never bleeds color into the next row. The shell fits
// every surface body with it, so the statusline always sits on the
// terminal's last row whatever a surface returns.
func Fit(block string, w, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(block, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		lines[i] = clipLine(l, w)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// clipLine cuts l to w cells, resetting SGR state when it had to cut.
func clipLine(l string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.Width(l) <= w {
		return l
	}
	return ansi.Clip(l, w) + "\x1b[0m"
}
