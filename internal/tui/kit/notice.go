package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// NoticeKind picks a Notice's color and glyph.
type NoticeKind int

const (
	NoticeError NoticeKind = iota
	NoticeWarning
	NoticeInfo
)

// Notice is the shared "this part can't work right now" block (F-055, the
// M24 deferral): what is wrong, why, and the one thing that fixes it.
// A bare "(store unavailable)" names a symptom; a Notice tells the user
// what to do next.
type Notice struct {
	Kind  NoticeKind
	What  string // short headline: "Tasks are unavailable"
	Why   string // optional cause, word-wrapped
	Retry string // optional fix: "run dhi doctor", "press r to retry"
}

// Lines renders the notice centered within width × height (height 0 =
// natural height); every row is exactly width cells.
func (n Notice) Lines(width, height int) []string {
	glyph, style := theme.GlyphCross, theme.DangerText()
	switch n.Kind {
	case NoticeWarning:
		glyph, style = "▲", theme.WarningText()
	case NoticeInfo:
		glyph, style = "●", theme.InfoText()
	}
	return EmptyState{
		Glyph:      style.Render(glyph),
		Title:      n.What,
		Why:        n.Why,
		Action:     n.Retry,
		glyphReady: true,
	}.Lines(width, height)
}

// String renders the notice at its natural height.
func (n Notice) String(width int) string {
	return strings.Join(n.Lines(width, 0), "\n")
}

// Loading is the shared in-progress block: a spinner (static under
// reduced motion), a label, and skeleton rows hinting at the shape of what
// is coming, so a slow load reads as progress rather than an empty pane.
func Loading(label string, frame, width, rows int) []string {
	out := []string{SpinnerGlyph(frame) + " " + theme.TextDim().Render(label), ""}
	sk := theme.TextMuted()
	for i := 0; i < rows; i++ {
		w := max(min(width-2, 64)-(i*7)%23, 8)
		out = append(out, "  "+sk.Render(strings.Repeat("▁", w)))
	}
	return out
}
