package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// RailRow is one nav sidebar row.
type RailRow struct {
	Glyph string // leading marker before Label ("" = none), e.g. theme.GlyphBranch
	Label string
	Count string // right-aligned; "" = none
	Badge string // pre-rendered suffix (e.g. unread dot); width-aware
}

// Rail is the nav sidebar primitive (F-025 Part B): inset-shade rows,
// one styled string per row (SGR resets inside concatenated segments
// drop the row background), active row highlighted, optional foot.
// Workspace/ideator/reviewer/settings sidebars all render through it.
//
// Foot is its own last row (F-026 P1): the row budget shrinks instead
// of the foot overwriting a nav row. Fixed Height overflow scrolls —
// EnsureActive keeps the highlighted row visible; the scroll cue rides
// the foot row (no foot = rows stay budget-clipped, no cue).
type Rail struct {
	Title  string // optional header row ("" = none)
	Rows   []RailRow
	Active int // highlighted row index; -1 = none
	Width  int
	Height int    // padded to this many rows; 0 = content
	Foot   string // pre-rendered foot line (flash etc.), own last row

	offset int
}

// View renders the rail, padded to Height (or the row count when 0).
func (r *Rail) View() string {
	r.ensureActive()

	h := r.Height
	if h <= 0 {
		h = r.rowsAvail() + (boolInt(r.Title != "") + boolInt(r.Foot != ""))
	}

	var lines []string
	if r.Title != "" {
		lines = append(lines, theme.RailMuted().Render(
			padTo(" "+strings.ToUpper(r.Title), r.Width)))
	}
	start, end := r.window()
	for i := start; i < end; i++ {
		lines = append(lines, r.renderRow(i))
	}
	for len(lines) < h-boolInt(r.Foot != "") {
		lines = append(lines, theme.RailMuted().Render(strings.Repeat(" ", r.Width)))
	}
	if r.Foot != "" && h >= 1 {
		ind := r.cue()
		lines = append(lines, theme.RailMuted().Render(
			padTo(r.Foot, r.Width-ansi.Width(ind)))+ind)
	}
	return strings.Join(lines[:h], "\n")
}

// renderRow renders row i (marker, optional glyph, label + tail).
func (r *Rail) renderRow(i int) string {
	row := r.Rows[i]
	marker := "  "
	if i == r.Active {
		marker = theme.GlyphCursor + " "
	}
	label := row.Label
	if row.Glyph != "" {
		label = row.Glyph + " " + label
	}
	tail := row.Count + row.Badge
	tailW := runeWidth(ansi.Strip(tail))
	plain := padTo(marker+label, r.Width-tailW) + tail
	if i == r.Active {
		return theme.TabActive().Render(plain)
	}
	return theme.RailDim().Render(plain)
}

// rowsAvail is the row budget inside Height: title + foot excluded.
func (r *Rail) rowsAvail() int {
	if r.Height <= 0 {
		return len(r.Rows)
	}
	n := r.Height - boolInt(r.Title != "") - boolInt(r.Foot != "")
	return clamp(n, 0, r.Height)
}

func (r *Rail) window() (start, end int) {
	n := r.rowsAvail()
	if n <= 0 || len(r.Rows) <= n {
		return 0, len(r.Rows)
	}
	end = r.offset + n
	if end > len(r.Rows) {
		end = len(r.Rows)
	}
	return r.offset, end
}

// ensureActive scrolls the window so the highlighted row is visible.
func (r *Rail) ensureActive() {
	n := r.rowsAvail()
	if n <= 0 || len(r.Rows) <= n {
		r.offset = 0
		return
	}
	i := clamp(r.Active, 0, len(r.Rows)-1)
	if i < r.offset {
		r.offset = i
	}
	if i >= r.offset+n {
		r.offset = i - n + 1
	}
}

// cue renders the scroll indicator ("", "▲", "▼", "▲▼") when rows
// overflow the budget; it rides the foot row.
func (r *Rail) cue() string {
	n := r.rowsAvail()
	s := Scroller{Total: len(r.Rows), Height: n, offset: r.offset}
	return s.Indicator()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
