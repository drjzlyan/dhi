package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// RailRow is one nav sidebar row.
type RailRow struct {
	Label string
	Count string // right-aligned; "" = none
	Badge string // pre-rendered suffix (e.g. unread dot); width-aware
}

// Rail is the nav sidebar primitive (F-025 Part B): inset-shade rows,
// one styled string per row (SGR resets inside concatenated segments
// drop the row background), active row highlighted, optional foot.
// Workspace/ideator/reviewer/settings sidebars all render through it.
type Rail struct {
	Title  string // optional header row ("" = none)
	Rows   []RailRow
	Active int // highlighted row index; -1 = none
	Width  int
	Height int    // padded to this many rows; 0 = content
	Foot   string // pre-rendered foot line (flash etc.), above the last row
}

// View renders the rail, padded to Height (or the row count when 0).
func (r *Rail) View() string {
	h := r.Height
	if h <= 0 {
		h = len(r.Rows)
		if r.Title != "" {
			h++
		}
		if r.Foot != "" {
			h++
		}
	}

	var lines []string
	if r.Title != "" {
		lines = append(lines, theme.RailMuted().Render(
			padTo(" "+strings.ToUpper(r.Title), r.Width)))
	}
	for i, row := range r.Rows {
		marker := "  "
		if i == r.Active {
			marker = theme.GlyphCursor + " "
		}
		tail := row.Count + row.Badge
		tailW := runeWidth(ansi.Strip(tail))
		plain := padTo(marker+row.Label, r.Width-tailW) + tail
		if i == r.Active {
			lines = append(lines, theme.TabActive().Render(plain))
		} else {
			lines = append(lines, theme.RailDim().Render(plain))
		}
	}
	for len(lines) < h {
		lines = append(lines, theme.RailMuted().Render(strings.Repeat(" ", r.Width)))
	}
	if r.Foot != "" && h >= 1 {
		lines[h-1] = theme.RailMuted().Render(padTo(r.Foot, r.Width))
	}
	return strings.Join(lines[:h], "\n")
}
