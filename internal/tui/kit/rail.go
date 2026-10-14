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
	Group string // a heading drawn above this row when it starts a new group (F-093)
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
	disp := r.display()
	start, end := r.window()
	for i := start; i < end; i++ {
		if d := disp[i]; d < 0 {
			lines = append(lines, r.renderGroup(-d-1))
		} else {
			lines = append(lines, r.renderRow(d))
		}
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

// RowAt maps a rail-local row y (0 = top of the rail) to the index of the
// nav row drawn there, accounting for the title row and the scroll
// window. ok is false over the title, the foot, blank fill or past the
// last row. Mouse clicks ride it (F-041).
func (r *Rail) RowAt(y int) (int, bool) {
	r.ensureActive()
	y -= boolInt(r.Title != "")
	if y < 0 {
		return -1, false
	}
	start, end := r.window()
	if i := start + y; i < end {
		if d := r.display()[i]; d >= 0 {
			return d, true
		}
	}
	return -1, false // a group heading, or past the last row
}

// display lists what each rail line shows: a row index, or -(row+1) for
// the group heading drawn above that row. Without groups it is just the
// rows, so ungrouped rails behave exactly as before.
func (r *Rail) display() []int {
	out := make([]int, 0, len(r.Rows))
	prev := ""
	for i, row := range r.Rows {
		if row.Group != "" && row.Group != prev {
			out = append(out, -(i + 1))
		}
		prev = row.Group
		out = append(out, i)
	}
	return out
}

// renderGroup draws the heading above row i as a labelled divider:
// "  agents ───" in the quiet rule color, so it organises without
// competing with the rows.
func (r *Rail) renderGroup(i int) string {
	label := "  " + strings.ToLower(r.Rows[i].Group) + " "
	fill := max(r.Width-ansi.Width(label)-1, 0)
	return theme.RailMuted().Render(label) +
		theme.RailMuted().Foreground(theme.Current.Rule).Render(strings.Repeat("─", fill)+" ")
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
	count := row.Count
	if count == "0" {
		count = "" // an empty section says so by having no count (F-093)
	}
	tail := count + row.Badge
	tailW := runeWidth(ansi.Strip(tail))
	plain := padTo(marker+label, r.Width-tailW) + tail
	if i == r.Active {
		return theme.RailActive().Render(plain)
	}
	return theme.RailDim().Render(plain)
}

// rowsAvail is the row budget inside Height: title + foot excluded.
func (r *Rail) rowsAvail() int {
	if r.Height <= 0 {
		return len(r.display())
	}
	n := r.Height - boolInt(r.Title != "") - boolInt(r.Foot != "")
	return clamp(n, 0, r.Height)
}

func (r *Rail) window() (start, end int) {
	n := r.rowsAvail()
	total := len(r.display())
	if n <= 0 || total <= n {
		return 0, total
	}
	end = r.offset + n
	if end > total {
		end = total
	}
	return r.offset, end
}

// ensureActive scrolls the window so the highlighted row is visible.
func (r *Rail) ensureActive() {
	n := r.rowsAvail()
	disp := r.display()
	if n <= 0 || len(disp) <= n {
		r.offset = 0
		return
	}
	active := clamp(r.Active, 0, len(r.Rows)-1)
	i := 0
	for j, d := range disp {
		if d == active {
			i = j
		}
	}
	top := i
	if i > 0 && disp[i-1] < 0 {
		top = i - 1 // keep the active row's heading in view with it
	}
	if top < r.offset {
		r.offset = top
	}
	if i >= r.offset+n {
		r.offset = i - n + 1
	}
}

// cue renders the scroll indicator ("", "▲", "▼", "▲▼") when rows
// overflow the budget; it rides the foot row.
func (r *Rail) cue() string {
	n := r.rowsAvail()
	s := Scroller{Total: len(r.display()), Height: n, offset: r.offset}
	return s.Indicator()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
