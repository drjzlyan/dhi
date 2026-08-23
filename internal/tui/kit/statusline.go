package kit

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// StatusSegment is one styled chunk of the statusline.
type StatusSegment struct {
	Text  string
	Style lipgloss.Style // zero value renders plain
}

// StatusLine is the bottom information bar: mode + context on the left,
// center info, key hints on the right.
type StatusLine struct {
	Left    []StatusSegment
	Center  string
	Hints   []string
	Version string
	Width   int
}

// DefaultStatusLine returns the M0 statusline for the given surface name.
func DefaultStatusLine(surfaceName string) *StatusLine {
	mode := theme.TabInactive().Background(theme.Current.BgSelection).
		Foreground(theme.Current.Accent).Bold(true)
	return &StatusLine{
		Left: []StatusSegment{
			{Text: " NORMAL ", Style: mode},
			{Text: " " + surfaceName},
		},
		Hints: []string{"1-9 switch", "tab next", "? help", "^c quit"},
	}
}

// View renders the statusline padded to Width cells when Width > 0.
func (s *StatusLine) View() string {
	bar := theme.StatusBar()
	hint := theme.Hint()

	var left string
	for _, seg := range s.Left {
		left += seg.Style.Render(seg.Text)
	}
	var right string
	for i, h := range s.Hints {
		if i > 0 {
			right += hint.Render(theme.GlyphBullet)
		}
		right += hint.Render(" " + h + " ")
	}

	total := runeWidth(ansi.Strip(left)) + runeWidth(ansi.Strip(s.Center)) + runeWidth(ansi.Strip(right))
	gap := 0
	if s.Width > total {
		gap = s.Width - total
	}
	lg := gap / 2
	mid := strings.Repeat(" ", lg) + s.Center

	out := left + mid + strings.Repeat(" ", gap-lg) + right
	out = padTo(out, s.Width)
	return bar.Render(out)
}
