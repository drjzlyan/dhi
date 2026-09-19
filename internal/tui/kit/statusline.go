package kit

import (
	"strings"

	"charm.land/lipgloss/v2"

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

// DefaultStatusLine returns the neutral base statusline for the given
// surface name: the surface label on the left, the global keys on the
// right. Contextual surfaces layer mode chips + zone + their own hints
// on top (F-025 Part C).
func DefaultStatusLine(surfaceName string) *StatusLine {
	return &StatusLine{
		Left:  []StatusSegment{{Text: " " + surfaceName, Style: theme.TextDim()}},
		Hints: []string{"1-5 views", "tab next", "? help", "^c quit"},
	}
}

// ModeChip renders a statusline mode segment: accent on the selection
// background (INSERT / FIND / CHAT …).
func ModeChip(text string) StatusSegment {
	return StatusSegment{
		Text: " " + strings.ToUpper(text) + " ",
		Style: lipgloss.NewStyle().
			Background(theme.Current.BgSelection).
			Foreground(theme.Current.Accent).Bold(true),
	}
}

// View renders the statusline padded to Width cells when Width > 0.
// Overflow rule (F-026 P1): the center segment is dropped first, then
// hints are dropped from the start (quit survives), then left/right
// ellipsis-clip — the line never exceeds the terminal width.
func (s *StatusLine) View() string {
	bar := theme.StatusBar()
	hint := theme.Hint()

	var left string
	for _, seg := range s.Left {
		left += seg.Style.Render(seg.Text)
	}
	rightFor := func(hints []string) string {
		var right string
		for i, h := range hints {
			if i > 0 {
				right += hint.Render(theme.GlyphBullet)
			}
			// Keycap the key fragment; the description stays muted
			// (F-026 P7 — keys read at a glance).
			key, desc := h, ""
			if i := strings.Index(h, " "); i > 0 {
				key, desc = h[:i], h[i+1:]
			}
			right += theme.Keycap().Render(" " + key + " ")
			if desc != "" {
				right += hint.Render(" " + desc + " ")
			}
		}
		return right
	}

	hints := s.Hints
	right := rightFor(hints)
	center := s.Center
	over := func() bool {
		return runeWidth(left)+runeWidth(center)+runeWidth(right) > s.Width
	}
	if over() {
		center = ""
	}
	for over() && len(hints) > 1 {
		hints = hints[1:]
		right = rightFor(hints)
	}
	if over() {
		center = ""
		hints = nil
		if w := runeWidth(left) + runeWidth(right); w > s.Width {
			split := clamp(s.Width-runeWidth(right), 0, s.Width)
			if split > 1 {
				left = ClipEllipsis(left, split)
			} else {
				left = ""
			}
			if runeWidth(left)+runeWidth(right) > s.Width {
				right = ClipEllipsis(right, s.Width-runeWidth(left))
			}
		}
	}

	total := runeWidth(left) + runeWidth(center) + runeWidth(right)
	gap := 0
	if s.Width > total {
		gap = s.Width - total
	}
	lg := gap / 2
	mid := strings.Repeat(" ", lg) + center

	out := left + mid + strings.Repeat(" ", gap-lg) + right
	out = padTo(out, s.Width)
	return bar.Render(out)
}
