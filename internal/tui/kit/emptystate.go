package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// EmptyState is the shared "nothing here yet" block (F-041): a glyph, a
// title, one line of why-it's-empty, and the single action that fixes it.
// A bare "(none)" teaches nothing; this tells the user what the pane is
// for and what to press.
type EmptyState struct {
	Glyph  string // defaults to theme.GlyphSpark
	Title  string
	Why    string // optional explanation, word-wrapped
	Action string // optional, e.g. "press n to start a session"

	glyphReady bool // Glyph is pre-styled (Notice); render it as is
}

// Lines renders the block centered horizontally within width and, when
// height > 0, vertically (padded to exactly height rows). Every row is at
// most width cells.
func (e EmptyState) Lines(width, height int) []string {
	glyph := e.Glyph
	if glyph == "" {
		glyph = theme.GlyphSpark
	}
	inner := width - 4
	if inner > 56 {
		inner = 56
	}
	if inner < 12 {
		inner = 12
	}
	if !e.glyphReady {
		glyph = theme.AccentText().Render(glyph)
	}
	block := []string{glyph, theme.TextStyle().Render(e.Title)}
	if e.Why != "" {
		block = append(block, "")
		for _, l := range WrapWords(e.Why, inner) {
			block = append(block, theme.TextDim().Render(l))
		}
	}
	if e.Action != "" {
		block = append(block, "", theme.AccentText().Render(e.Action))
	}
	out := make([]string, 0, len(block)+height)
	if height > len(block) {
		for i := 0; i < (height-len(block))/3; i++ { // sit slightly above center
			out = append(out, "")
		}
	}
	for _, l := range block {
		w := ansi.Width(l)
		pad := (width - w) / 2
		if pad < 0 {
			pad = 0
		}
		out = append(out, strings.Repeat(" ", pad)+l)
	}
	for height > 0 && len(out) < height {
		out = append(out, "")
	}
	// Full-width rows: the host pane's background stays uniform instead
	// of changing where each line's text ends.
	for i, l := range out {
		out[i] = padTo(l, width)
	}
	return out
}
