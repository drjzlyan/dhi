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
	// Right is an optional right-aligned status chip (agent presence).
	// It is dropped, never clipped, when the bar is too narrow for it.
	Right string
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

// brand is the fixed tab-bar prefix.
const brand = " ◆ DHI "

// labels returns each tab's text for the bar's width (F-054): full
// labels when they fit, else the active tab keeps its name and the rest
// show only their number, else numbers only — the bar shrinks before it
// ever clips.
func (t *Tabs) labels() []string {
	full := make([]string, len(t.Items))
	activeOnly := make([]string, len(t.Items))
	numbers := make([]string, len(t.Items))
	for i, it := range t.Items {
		n := strconv.Itoa(i + 1)
		full[i] = n + " " + it.Label
		numbers[i] = n
		activeOnly[i] = n
		if i == t.Active {
			activeOnly[i] = full[i]
		}
	}
	for _, set := range [][]string{full, activeOnly} {
		if t.Width <= 0 || barWidth(set) <= t.Width {
			return set
		}
	}
	return numbers
}

// barWidth is the visible width of brand + tabs + separators.
func barWidth(labels []string) int {
	w := runeWidth(brand)
	for i, l := range labels {
		w += runeWidth(" " + l + " ")
		if i > 0 {
			w += runeWidth(theme.GlyphChevron)
		}
	}
	return w
}

// View renders "1 Home  2 Editor …" with the active tab highlighted and the
// whole bar padded to Width cells.
func (t *Tabs) View() string {
	active := theme.TabCurrent()
	inactive := theme.TabInactive()

	var parts []string
	for i, label := range t.labels() {
		st := inactive
		if i == t.Active {
			st = active
		}
		parts = append(parts, st.Render(" "+label+" "))
	}

	left := theme.Brand().Render(brand) + strings.Join(parts, theme.Hint().Render(theme.GlyphChevron))
	if t.Right != "" && t.Width > 0 {
		lw, rw := runeWidth(ansi.Strip(left)), runeWidth(ansi.Strip(t.Right))
		if lw+2+rw <= t.Width {
			left += strings.Repeat(" ", t.Width-lw-rw-1) + t.Right + " "
		}
	}
	line := padTo(left, t.Width)
	// Even numbers-only can exceed a tiny terminal: clip visibly (F-026 P1).
	return ClipEllipsis(line, t.Width)
}

// Hit returns the tab index under the tab-bar column x, using the same
// layout math as View (brand prefix, separator, clipped tail). Mouse
// click routing rides it (F-026 P2); -1 when x falls on no tab.
func (t *Tabs) Hit(x int) (i int, ok bool) {
	if x < 0 || len(t.Items) == 0 {
		return -1, false
	}
	start := runeWidth(brand)
	for j, label := range t.labels() {
		end := start + runeWidth(" "+label+" ") + runeWidth(theme.GlyphChevron)
		if x < end {
			if x >= start {
				return j, true
			}
			return -1, false
		}
		start = end
	}
	return -1, false
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
