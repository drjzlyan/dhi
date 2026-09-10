package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Layout breakpoints shared by every surface (F-025 Part D).
const (
	// WCompact: below this the centered fallback is the only option.
	WCompact = 60
	// WDock: below this surfaces render a full-width vertical stack;
	// at/above it they dock a rail beside the main pane.
	WDock = 84
	// WWide: at/above this, surfaces open extra side panes (board
	// detail, chat context).
	WWide = 120
)

// HintBar renders the bottom chrome row (F-025 Part A): an optional
// status segment (pre-rendered via theme.ChromeStatus — flash messages
// keep their semantic color) followed by the muted keymap, right-
// padded on the chrome background. Instructions live here, at the
// bottom, never the focus. The row is exactly width cells: the keymap
// clips (styles preserved) when the status leaves no room.
func HintBar(width int, status string, hints ...string) string {
	bar := theme.ChromeBar()
	sep := " " + theme.GlyphBullet + " "
	statusW := runeWidth(ansi.Strip(status))
	gap := 0
	if status != "" {
		gap = 2
	}
	hintText := ansi.Clip(strings.Join(hints, sep), width-statusW-gap)
	if hintText == "" && status == "" {
		hintText = ansi.Clip(strings.Join(hints, sep), width)
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
