package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// HitMap records clickable zones while a view renders (F-055, the M24
// "clicks inside lists and lanes" deferral). Render code knows where it
// put each row; recording zones at that moment means a click is resolved
// against exactly what is on screen — no second copy of the layout math
// that could drift from the real one.
//
// Usage: Reset at the top of View, SetOrigin before rendering a nested
// block (its zones are then relative to that block), Add per clickable
// rectangle, and Click from the surface's Click seam.
type HitMap struct {
	zones  []hitZone
	ox, oy int
}

type hitZone struct {
	x, y, w, h int
	fn         func(dx, dy int)
}

// Reset drops every zone and the origin (call once per frame).
func (m *HitMap) Reset() { m.zones, m.ox, m.oy = m.zones[:0], 0, 0 }

// SetOrigin makes later Add coordinates relative to (x, y).
func (m *HitMap) SetOrigin(x, y int) { m.ox, m.oy = x, y }

// Add registers a w×h zone at (x, y) relative to the origin; fn receives
// the click's offset inside the zone. Later zones win over earlier ones
// (overlays register last).
func (m *HitMap) Add(x, y, w, h int, fn func(dx, dy int)) {
	if w <= 0 || h <= 0 || fn == nil {
		return
	}
	m.zones = append(m.zones, hitZone{x: m.ox + x, y: m.oy + y, w: w, h: h, fn: fn})
}

// Click runs the topmost zone under (x, y); false when none.
func (m *HitMap) Click(x, y int) bool {
	for i := len(m.zones) - 1; i >= 0; i-- {
		z := m.zones[i]
		if x >= z.x && x < z.x+z.w && y >= z.y && y < z.y+z.h {
			z.fn(x-z.x, y-z.y)
			return true
		}
	}
	return false
}

// SectionStrip renders the compact one-line section switcher used below
// the dock width ("INBOX · [BOARD] · CHANNELS") and returns each label's
// [start, end) column span so callers can register click zones.
func SectionStrip(labels []string, active int) (string, [][2]int) {
	sep := theme.TextDim().Render(" · ")
	var b strings.Builder
	spans := make([][2]int, len(labels))
	col := 0
	for i, l := range labels {
		if i > 0 {
			b.WriteString(sep)
			col += 3
		}
		text := l
		if i == active {
			text = "[" + l + "]"
			b.WriteString(theme.TabActive().Render(text))
		} else {
			b.WriteString(theme.TextDim().Render(text))
		}
		w := runeWidth(text)
		spans[i] = [2]int{col, col + w}
		col += w
	}
	return b.String(), spans
}

// AddRows registers one zone per visible list item for a windowed list:
// items start..len(heights)-1 stack from row y0 (heights[i] rows each,
// at most maxRows in total), each width w. fn gets the item index.
func (m *HitMap) AddRows(y0, w, start int, heights []int, maxRows int, fn func(i int)) {
	y := y0
	for i := start; i < len(heights) && y-y0 < maxRows; i++ {
		item := i
		m.Add(0, y, w, min(heights[i], maxRows-(y-y0)), func(int, int) { fn(item) })
		y += heights[i]
	}
}

// Ones is a heights slice of n single-row items (for AddRows).
func Ones(n int) []int {
	h := make([]int, n)
	for i := range h {
		h[i] = 1
	}
	return h
}
