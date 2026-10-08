// Package vt is a minimal VT interpreter for terminal scrollback
// (F-026 P5): it keeps raw SGR sequences inline (colors are session
// CONTENT, passthrough — the theme-only rule governs DHI chrome, not
// what a CLI prints), interprets cursor movement and erases so
// redraws/progress lines land correctly, and grows history downward.
// Deliberately not a full terminal: alt-screens, scroll regions, and
// private modes are ignored (the drawer is a scrollback viewer, not a
// full-screen app host).
package vt

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Screen holds one session's scrollback.
type Screen struct {
	lines []string
	row   int // absolute cursor line
	col   int // visible cursor column
	cap   int

	savedRow, savedCol int
}

// New returns a screen with the given scrollback capacity.
func New(capacity int) *Screen {
	if capacity < 10 {
		capacity = 10
	}
	return &Screen{cap: capacity, row: 0}
}

// Feed ingests one output chunk.
func (s *Screen) Feed(chunk []byte) {
	i := 0
	for i < len(chunk) {
		b := chunk[i]
		switch {
		case b == 0x1b: // ESC
			i = s.escape(chunk, i)
		case b == '\r':
			s.col = 0
			i++
		case b == '\n':
			s.newline()
			i++
		case b == '\t':
			s.tab()
			i++
		case b == 0x08: // BS
			if s.col > 0 {
				s.col--
			}
			i++
		case b == 0x07: // BEL
			i++
		case b < 0x20:
			i++ // other control bytes: ignore
		default:
			r, sz := utf8.DecodeRune(chunk[i:])
			s.write(r)
			i += sz
		}
	}
	s.trimCap()
}

// newline advances the cursor; history grows lazily on the next write
// (a trailing newline leaves the cursor on an unmaterialized row).
func (s *Screen) newline() {
	s.row++
	s.col = 0
}

func (s *Screen) tab() {
	next := (s.col/8 + 1) * 8
	s.padTo(next)
	s.col = next
}

// padTo space-fills the cursor line to at least n visible cells.
func (s *Screen) padTo(n int) {
	if s.row >= len(s.lines) {
		for s.row >= len(s.lines) {
			s.lines = append(s.lines, "")
		}
	}
	w := Width(s.lines[s.row])
	if w < n {
		s.lines[s.row] += strings.Repeat(" ", n-w)
	}
}

// write puts one rune at the cursor: append past the line end,
// clip-and-replace overwrite otherwise (CR-repainted progress lines).
func (s *Screen) write(r rune) {
	if s.row >= len(s.lines) {
		for s.row >= len(s.lines) {
			s.lines = append(s.lines, "")
		}
	}
	line := s.lines[s.row]
	w := Width(line)
	if s.col >= w {
		if s.col > w {
			s.padTo(s.col)
			line = s.lines[s.row]
		}
		s.lines[s.row] = line + string(r)
	} else {
		// Overwrite: keep the styled prefix, drop the overwritten tail.
		s.lines[s.row] = Clip(line, s.col) + string(r)
	}
	s.col += runewidth.RuneWidth(r)
}

// escape consumes one escape sequence at chunk[i:], returning the next
// index. SGR sequences pass through into the current line so styling
// carries; positioning sequences move the cursor; the rest are ignored.
func (s *Screen) escape(chunk []byte, i int) int {
	if i+1 >= len(chunk) {
		return len(chunk)
	}
	switch chunk[i+1] {
	case '(', ')', '*', '+':
		return i + 3 // charset designators: ESC ( B
	case '[': // CSI
		j := i + 2
		for j < len(chunk) && (chunk[j] < 0x40 || chunk[j] > 0x7e) {
			j++
		}
		if j >= len(chunk) {
			return len(chunk) // incomplete: dropped (chunk-boundary only)
		}
		s.csi(string(chunk[i+2:j]), chunk[j])
		return j + 1
	case ']': // OSC — swallow to BEL or ST
		j := i + 2
		for j < len(chunk) {
			if chunk[j] == 0x07 {
				return j + 1
			}
			if chunk[j] == 0x1b && j+1 < len(chunk) && chunk[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return len(chunk)
	}
	return i + 3
}

// csi applies one control sequence (params + final byte).
func (s *Screen) csi(params string, final byte) {
	// n is the count parameter; absent, zero or malformed means 1.
	n := func() int {
		v := 0
		for _, c := range params {
			if c >= '0' && c <= '9' {
				v = v*10 + int(c-'0')
			} else {
				return 1
			}
		}
		if v == 0 {
			return 1
		}
		return v
	}
	s.ensureRow()
	switch final {
	case 'm', 'h', 'l': // SGR passes through; set/reset modes ignored
		if final == 'm' {
			s.lines[s.row] += "\x1b[" + params + "m"
		}
	case 'A':
		s.row = max(s.row-n(), 0)
	case 'B', 'e':
		s.row += n()
		s.ensureRow()
	case 'C':
		s.col += n()
	case 'D':
		s.col = max(s.col-n(), 0)
	case 'G', '`':
		s.col = max(n()-1, 0)
	case 'H', 'f':
		row, col := 1, 1
		parts := strings.SplitN(params, ";", 2)
		if v := atoi(parts[0]); v > 0 {
			row = v
		}
		if len(parts) == 2 {
			if v := atoi(parts[1]); v > 0 {
				col = v
			}
		}
		s.row = max(row-1, 0)
		s.ensureRow()
		s.col = max(col-1, 0)
	case 'E':
		s.row += n()
		s.col = 0
		s.ensureRow()
	case 'F':
		s.row = max(s.row-n(), 0)
		s.col = 0
	case 'd':
		s.row = max(n()-1, 0)
		s.ensureRow()
	case 'J':
		s.eraseDisplay(atoi(params))
	case 'K':
		s.eraseLine(atoi(params))
	case 's':
		s.savedRow, s.savedCol = s.row, s.col
	case 'u':
		s.row, s.col = s.savedRow, s.savedCol
		s.ensureRow()
	}
}

// ensureRow grows history so the cursor row exists.
func (s *Screen) ensureRow() {
	for s.row >= len(s.lines) {
		s.lines = append(s.lines, "")
	}
	if s.row < 0 {
		s.row = 0
	}
}

// eraseLine: 0 cursor→EOL, 1 SOF→cursor, 2 whole line.
func (s *Screen) eraseLine(mode int) {
	if s.row >= len(s.lines) {
		return
	}
	line := s.lines[s.row]
	switch mode {
	case 0:
		s.lines[s.row] = Clip(line, s.col)
	case 1:
		s.lines[s.row] = strings.Repeat(" ", max(s.col-Width(Clip(line, s.col)), 0)) +
			line[len(Clip(line, s.col)):]
	case 2:
		s.lines[s.row] = ""
	}
}

// eraseDisplay: 0 cursor→end, 1 start→cursor, 2 all.
func (s *Screen) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseLine(0)
		for i := s.row + 1; i < len(s.lines); i++ {
			s.lines[i] = ""
		}
	case 1:
		for i := 0; i < s.row; i++ {
			s.lines[i] = ""
		}
		s.eraseLine(1)
	case 2:
		s.lines = []string{""}
		s.row = 0
		s.col = 0
	}
}

// trimCap keeps history within capacity, keeping the cursor in range.
func (s *Screen) trimCap() {
	if len(s.lines) <= s.cap {
		return
	}
	drop := len(s.lines) - s.cap
	s.lines = s.lines[drop:]
	s.row = max(s.row-drop, 0)
}

// Pending reports whether the cursor sits on an unmaterialized row
// (a fresh prompt line after the last newline).
func (s *Screen) Pending() bool { return s.row >= len(s.lines) }

// Lines renders the tail window: the last height lines clipped to
// width, styles intact.
func (s *Screen) Lines(width, height int) []string {
	if height <= 0 {
		return nil
	}
	start := max(len(s.lines)-height, 0)
	out := make([]string, 0, height)
	for _, l := range s.lines[start:] {
		out = append(out, Clip(l, width))
	}
	return out
}

// Width measures visible display cells of a passthrough line.
func Width(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if j := escapeEnd(s, i); j > i {
				i = j
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		n += runewidth.RuneWidth(r)
		i += sz
	}
	return n
}

// Clip keeps the first n visible cells, escapes intact.
func Clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if j := escapeEnd(s, i); j > i {
				b.WriteString(s[i:j])
				i = j
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		rw := runewidth.RuneWidth(r)
		if w+rw > n {
			break
		}
		b.WriteRune(r)
		w += rw
		i += sz
	}
	return b.String()
}

// escapeEnd returns the index just past the escape sequence at i.
func escapeEnd(s string, i int) int {
	if i+1 >= len(s) {
		return i
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j < len(s) {
			return j + 1
		}
	case ']':
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
			j++
		}
	default:
		return i + 2
	}
	return i
}

func atoi(s string) int {
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int(c-'0')
	}
	return v
}
