package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Layout breakpoints shared by every surface (F-025 Part D). The
// definition moved to theme (F-026 P1 — theme owns the responsive
// identity); these aliases keep kit consumers compiling.
const (
	// WCompact: below this the centered fallback is the only option.
	WCompact = theme.WCompact
	// WDock: below this surfaces render a full-width vertical stack;
	// at/above it they dock a rail beside the main pane.
	WDock = theme.WDock
	// WWide: at/above this, surfaces open extra side panes (board
	// detail, chat context).
	WWide = theme.WWide
)

// HintBar renders the bottom chrome row (F-025 Part A): an optional
// status segment (pre-rendered via theme.ChromeStatus — flash messages
// keep their semantic color) followed by the muted keymap, right-
// padded on the chrome background. Instructions live here, at the
// bottom, never the focus. The row is exactly width cells: the keymap
// clips with an ellipsis marker (styles preserved) when the status
// leaves no room — truncation is visible, never silent (F-026).
func HintBar(width int, status string, hints ...string) string {
	bar := theme.ChromeBar()
	sep := " " + theme.GlyphBullet + " "
	statusW := runeWidth(ansi.Strip(status))
	gap := 0
	if status != "" {
		gap = 2
	}
	shown := make([]string, len(hints))
	for i, h := range hints {
		shown[i] = DisplayHint(h) // the key the user presses (F-058 remaps)
	}
	full := strings.Join(shown, sep)
	hintText := ClipEllipsis(full, width-statusW-gap)
	if hintText == "" && status == "" {
		hintText = ClipEllipsis(full, width)
	}
	row := status
	if status != "" && hintText != "" {
		row += strings.Repeat(" ", gap)
	}
	if hintText != "" {
		row += bar.Render(hintText)
	}
	if w := runeWidth(ansi.Strip(row)); w < width {
		row += bar.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// HintKeyAt maps a click at column x of a HintBar(width, status, hints...)
// row to the key its hint names (F-055: the keymap is clickable). The key
// is the first alternative of the hint's key part ("s/S status" → "s",
// "[ ] sections" → "[", "space mark" → " "). False on the status, a
// separator, or the clipped tail.
func HintKeyAt(width int, status string, x int, hints ...string) (string, bool) {
	col := runeWidth(ansi.Strip(status))
	if status != "" {
		col += 2
	}
	limit := width - 1 // the last visible cell may be the "…" marker
	for i, h := range hints {
		if i > 0 {
			col += 3 // " • "
		}
		w := runeWidth(DisplayHint(h)) // measured as shown, acted on as logical
		if x >= col && x < col+w && x < limit {
			rows := HintRows(h)
			if len(rows) == 0 {
				return "", false
			}
			k := strings.Fields(strings.ReplaceAll(rows[0][0], "/", " "))[0]
			if k == "space" {
				k = " "
			}
			return k, true
		}
		col += w
	}
	return "", false
}
