package kit

import (
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Tabs is the top navigation bar showing numbered surfaces.
type Tabs struct {
	Items  []Tab
	Active int
	Width  int
}

// Tab is one entry in the navigation bar.
type Tab struct {
	ID    string // stable surface id
	Label string // short display label
}

// NewTabs builds a tab bar from id/label pairs.
func NewTabs(pairs ...[2]string) *Tabs {
	t := &Tabs{}
	for _, p := range pairs {
		t.Items = append(t.Items, Tab{ID: p[0], Label: p[1]})
	}
	return t
}

// SetActive selects by index, clamped to range. Returns true if changed.
func (t *Tabs) SetActive(i int) bool {
	if len(t.Items) == 0 {
		return false
	}
	c := clamp(i, 0, len(t.Items)-1)
	if c == t.Active {
		return false
	}
	t.Active = c
	return true
}

// ActiveID returns the selected tab's surface id.
func (t *Tabs) ActiveID() string {
	if t.Active < len(t.Items) {
		return t.Items[t.Active].ID
	}
	return ""
}

// View renders "1 Home  2 Editor …" with the active tab highlighted and the
// whole bar padded to Width cells.
func (t *Tabs) View() string {
	_ = theme.TabBar()
	active := theme.TabActive()
	inactive := theme.TabInactive()

	var parts []string
	for i, it := range t.Items {
		label := strconv.Itoa(i+1) + " " + it.Label
		st := inactive
		if i == t.Active {
			st = active
		}
		parts = append(parts, st.Render(" "+label+" "))
	}

	line := padTo(theme.Brand().Render(" ◆ DHI ")+strings.Join(parts, theme.Hint().Render(theme.GlyphChevron)), t.Width)
	return line
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// padTo right-pads a possibly-styled line to w visible cells when w > 0.
func padTo(line string, w int) string {
	if w <= 0 {
		return line
	}
	if vis := runeWidth(ansi.Strip(line)); vis < w {
		line += strings.Repeat(" ", w-vis)
	}
	return line
}
