package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Button is one clickable action: the key that does it and what it does
// (F-093). It renders as a filled face in its variant's role color
// (F-096), with the key underlined inside: " y Approve ".
type Button struct {
	Key     string
	Label   string
	Variant theme.ButtonVariant // zero value: primary
	// Focused bolds the face (the action enter would take); Disabled
	// mutes it and Buttons gives it no click span.
	Focused  bool
	Disabled bool
}

// Width is the face's width in cells.
func (b Button) Width() int {
	if b.Key == "" {
		return runeWidth(b.Label) + 2
	}
	return runeWidth(b.Key) + runeWidth(b.Label) + 3
}

// Render draws the face.
func (b Button) Render() string {
	face := theme.Button(b.Variant, b.Focused, b.Disabled)
	if b.Key == "" {
		return face.Render(" " + b.Label + " ")
	}
	return face.Render(" ") +
		theme.ButtonKeyOn(b.Variant, b.Focused, b.Disabled).Render(b.Key) +
		face.Render(" "+b.Label+" ")
}

// ButtonSpan is where a rendered button sits: line within the block and
// its column range, for click routing.
type ButtonSpan struct {
	Line, From, To int
	Key            string
}

// Buttons lays buttons out left to right, two cells apart, wrapping to
// width — a button never splits across lines. Disabled buttons get no
// span.
func Buttons(bs []Button, width int) ([]string, []ButtonSpan) {
	var lines []string
	var spans []ButtonSpan
	cur, curW := "", 0
	for _, b := range bs {
		w := b.Width()
		gap := 0
		if curW > 0 {
			gap = 2
		}
		if curW > 0 && curW+gap+w > width {
			lines = append(lines, cur)
			cur, curW, gap = "", 0, 0
		}
		cur += strings.Repeat(" ", gap) + b.Render()
		if !b.Disabled {
			spans = append(spans, ButtonSpan{Line: len(lines), From: curW + gap, To: curW + gap + w, Key: b.Key})
		}
		curW += gap + w
	}
	if curW > 0 {
		lines = append(lines, cur)
	}
	return lines, spans
}
