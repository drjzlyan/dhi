package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Button is one clickable action in a pane: the key that does it and
// what it does (F-093 — context panes offer their actions as buttons).
type Button struct {
	Key   string
	Label string
}

// ButtonSpan is where a rendered button sits: line within the block and
// its column range, for click routing.
type ButtonSpan struct {
	Line, From, To int
	Key            string
}

// Buttons lays buttons out left to right as "[y] yes  [n] no", wrapping
// to width — a button never splits across lines.
func Buttons(bs []Button, width int) ([]string, []ButtonSpan) {
	var lines []string
	var spans []ButtonSpan
	cur, curW := "", 0
	for _, b := range bs {
		face := theme.ButtonKey().Render(" "+b.Key+" ") + " " + theme.TextStyle().Render(b.Label)
		w := runeWidth(" "+b.Key+" ") + 1 + runeWidth(b.Label)
		gap := 0
		if curW > 0 {
			gap = 2
		}
		if curW > 0 && curW+gap+w > width {
			lines = append(lines, cur)
			cur, curW, gap = "", 0, 0
		}
		cur += strings.Repeat(" ", gap) + face
		spans = append(spans, ButtonSpan{Line: len(lines), From: curW + gap, To: curW + gap + w, Key: b.Key})
		curW += gap + w
	}
	if curW > 0 {
		lines = append(lines, cur)
	}
	return lines, spans
}
