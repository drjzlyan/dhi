package kit

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Scroller owns the offset of a vertical scroll window. Surfaces keep
// cursors and re-render full rows; the scroller keeps the window in
// sync (list semantics) and provides position cues and a scrollbar
// column. Height 0 = the window is the full content (offset stays 0).
// Cursor movement routes through EnsureVisible so the cursor can never
// walk into clipped rows invisibly (F-026 P1).
type Scroller struct {
	Total  int // total content rows
	Height int // visible rows
	offset int
}

// Window returns the visible row range: start inclusive, end exclusive.
func (s *Scroller) Window() (start, end int) {
	if s.Height <= 0 || s.Height >= s.Total {
		return 0, s.Total
	}
	end = s.offset + s.Height
	if end > s.Total {
		end = s.Total
	}
	return s.offset, end
}

// Scroll moves the window by dy rows (negative = up), clamped.
func (s *Scroller) Scroll(dy int) {
	if s.Height <= 0 || s.Total <= s.Height {
		s.offset = 0
		return
	}
	s.offset = clamp(s.offset+dy, 0, s.Total-s.Height)
}

// PageUp / PageDown / Top / Bottom are the canonical page motions.
func (s *Scroller) PageUp()   { s.Scroll(-s.page()) }
func (s *Scroller) PageDown() { s.Scroll(s.page()) }
func (s *Scroller) Top()      { s.offset = 0 }
func (s *Scroller) Bottom()   { s.Scroll(s.Total) }

func (s *Scroller) page() int {
	if s.Height > 1 {
		return s.Height - 1
	}
	return 1
}

func (s *Scroller) windowH() int {
	if s.Height > 0 {
		return s.Height
	}
	return s.Total
}

// EnsureVisible keeps row i inside the window (cursor-follow).
func (s *Scroller) EnsureVisible(i int) {
	if s.Height <= 0 || s.Total <= s.Height {
		return
	}
	if i < s.offset {
		s.offset = i
	}
	if i >= s.offset+s.Height {
		s.offset = i - s.Height + 1
	}
}

// SetTotal replaces the content-row count, clamping the offset.
func (s *Scroller) SetTotal(n int) {
	s.Total = n
	s.Scroll(0)
}

// Cues report whether content sits above/below the window.
func (s *Scroller) Cues() (up, down bool) {
	up = s.offset > 0
	down = s.windowH() < s.Total && s.offset+s.windowH() < s.Total
	return up, down
}

// Offset reads the window start (state restore at call sites).
func (s *Scroller) Offset() int { return s.offset }

// SetOffset moves the window start, clamped.
func (s *Scroller) SetOffset(i int) {
	if s.Height <= 0 || s.Total <= s.Height {
		s.offset = 0
		return
	}
	s.offset = clamp(i, 0, s.Total-s.Height)
}

// Indicator renders the inline position cue: "" when fully visible,
// "▲", "▼", or "▲▼" otherwise (panes without a scrollbar column).
func (s *Scroller) Indicator() string {
	up, down := s.Cues()
	if up && down {
		return "▲▼"
	}
	if up {
		return "▲"
	}
	if down {
		return "▼"
	}
	return ""
}

// Scrollbar renders a one-column thumb track of h rows: track glyphs
// on the border shade, the thumb on the dim accent. The result is
// always exactly h rows (no padding math at call sites).
func (s *Scroller) Scrollbar(h int) string {
	track := theme.TextMuted()
	thumb := theme.AccentDimText()
	rows := make([]string, 0, h)
	for y := 0; y < h; y++ {
		rows = append(rows, track.Render(scrollTrackGlyph))
	}
	if th := s.thumbLen(h); th > 0 {
		pos := s.thumbPos(h, th)
		for y := pos; y < pos+th; y++ {
			rows[y] = thumb.Render(scrollThumbGlyph)
		}
	}
	return strings.Join(rows, "\n")
}

func (s *Scroller) thumbLen(h int) int {
	if s.Total <= 0 || h <= 0 {
		return 0
	}
	n := clamp(s.windowH()*h/s.Total, 1, h)
	if n >= h && s.Total > s.windowH() {
		return h - 1
	}
	return n
}

func (s *Scroller) thumbPos(h, th int) int {
	span := s.Total - s.windowH()
	if span <= 0 {
		return 0
	}
	return clamp(s.offset*h/span, 0, h-th)
}
