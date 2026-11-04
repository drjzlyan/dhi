package editor

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/mattn/go-runewidth"
)

// Soft word wrap (F-136): long lines continue on the next row instead of
// scrolling sideways. Wrapping is display only; the buffer, j/k and every
// position stay on logical lines and runes.

// wrapEnabled is the effective wrap: a session :set wrap|nowrap beats
// the editor.wrap setting.
func (m *Model) wrapEnabled(p Prefs) bool {
	if m.wrapOverride != nil {
		return *m.wrapOverride
	}
	return p.Wrap
}

// wrapCommand handles :set wrap and :set nowrap (this session only; the
// setting itself lives in Settings).
func (m *Model) wrapCommand(cmd string) (string, bool) {
	var on bool
	switch strings.TrimSpace(cmd) {
	case "set wrap":
		on = true
	case "set nowrap":
		on = false
	default:
		return "", false
	}
	m.wrapOverride = &on
	if on {
		return "wrap: on", true
	}
	return "wrap: off", true
}

// wrapLayout is the window of lines a wrapped view shows and how each
// breaks into rows.
type wrapLayout struct {
	top, end int     // lines [top, end) are drawn
	segs     [][]int // per line, the first rune (tab-expanded) of each row
}

// lineSegs breaks buffer line l for the given width. The cursor line
// reserves a cell for the block when the cursor sits past the last rune.
func lineSegs(e *textbuf.Editor, l, width int, cells []inlayCell) []int {
	b := e.Buffer()
	plain := expandTabs(b.Line(l))
	if l == b.Cursor().Line && visCol(b.Line(l), b.Cursor().Col) >= len([]rune(plain)) {
		plain += " "
	}
	return wrapSegmentsHinted(plain, width, extraWidths(cells, len([]rune(plain))))
}

// layoutWrap picks the lines to draw so the cursor line sits near the
// middle of rows screen rows, filling to the bottom at the end of the
// buffer.
func (m *Model) layoutWrap(e *textbuf.Editor, rows, width int) *wrapLayout {
	b := e.Buffer()
	cur := b.Cursor().Line
	n := b.LineCount()
	cache := map[int][]int{}
	segs := func(l int) []int {
		if s, ok := cache[l]; ok {
			return s
		}
		s := lineSegs(e, l, width, m.inlayCells(e, l))
		cache[l] = s
		return s
	}
	top, used := cur, 0
	for top > 0 && used+len(segs(top-1)) <= rows/2 {
		top--
		used += len(segs(top))
	}
	end := cur
	for end < n && used < rows {
		used += len(segs(end))
		end++
	}
	for top > 0 && used+len(segs(top-1)) <= rows { // short of the end: use the room above
		top--
		used += len(segs(top))
	}
	wl := &wrapLayout{top: top, end: end}
	for l := top; l < end; l++ {
		wl.segs = append(wl.segs, segs(l))
	}
	return wl
}

// wrapSegments returns the rune index each row starts at (the first is
// 0). A row breaks after the last space that fits, or mid-word when the
// word is longer than the row. Wide runes are never split.
func wrapSegments(plain string, width int) []int { return wrapSegmentsHinted(plain, width, nil) }

// wrapSegmentsHinted is wrapSegments for a line carrying inlay hints
// (F-169): extra[i] is the width of the hints drawn before rune i, which
// take room in the row but never become break points or text.
func wrapSegmentsHinted(plain string, width int, extra []int) []int {
	rs := []rune(plain)
	hint := func(i int) int {
		if i < len(extra) {
			return extra[i]
		}
		return 0
	}
	starts := []int{0}
	start, w, lastSpace := 0, 0, -1
	text := false // the row holds more than indentation: a break there would leave an empty row
	for i, r := range rs {
		rw := runewidth.RuneWidth(r) + hint(i)
		if w+rw > width && i > start {
			brk := i
			if lastSpace > start {
				brk = lastSpace
			}
			starts = append(starts, brk)
			start = brk
			w = runesWidth(rs[brk:i])
			for j := brk; j < i; j++ {
				w += hint(j)
			}
			lastSpace = -1
			text = strings.TrimSpace(string(rs[brk:i])) != ""
		}
		w += rw
		if r == ' ' {
			if text {
				lastSpace = i + 1
			}
		} else {
			text = true
		}
	}
	return starts
}

// runesWidth is the display width of rs in cells.
func runesWidth(rs []rune) int {
	w := 0
	for _, r := range rs {
		w += runewidth.RuneWidth(r)
	}
	return w
}

// runeAtCellHinted is the rune index (in the tab-expanded line) under cell
// off of the row that starts at rune start; the row may show inlay hints
// (F-169), and a cell on a hint lands on the rune the hint precedes.
func runeAtCellHinted(plain string, start, off int, cells []inlayCell) int {
	rs := []rune(plain)
	w, hi := 0, 0
	for i := start; i < len(rs); i++ {
		for hi < len(cells) && cells[hi].idx < i {
			hi++
		}
		for j := hi; j < len(cells) && cells[j].idx == i; j++ {
			if off < w+cells[j].w {
				return i
			}
			w += cells[j].w
		}
		rw := runewidth.RuneWidth(rs[i])
		if off < w+rw {
			return i
		}
		w += rw
	}
	return len(rs)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
