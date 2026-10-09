// Package ansi provides terminal escape-sequence utilities shared by
// rendering and testing code.
package ansi

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

var re = regexp.MustCompile(`\x1b\[[0-9;:?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-B]`)

// Strip removes ANSI escape sequences, returning visible text only.
func Strip(s string) string { return re.ReplaceAllString(s, "") }

// Width returns the visible display width of s in cells, skipping
// escape sequences (raw or ANSI-stripped input both measure the same).
// This is the one width truth for rendering (ADR-0015).
func Width(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				i += loc[1]
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		n += runewidth.RuneWidth(r)
		i += sz
	}
	return n
}

// sgrBg reports how an SGR sequence ("\x1b[...m") leaves the background:
// touched is true when any parameter sets or clears it, set is the
// state after the last such parameter. Extended colors (38/48/58) skip
// their sub-parameters so "38;2;0;0;0" never reads as a reset.
func sgrBg(seq string) (touched, set bool) {
	if len(seq) < 3 || seq[len(seq)-1] != 'm' || seq[1] != '[' {
		return false, false
	}
	body := seq[2 : len(seq)-1]
	if body == "" {
		return true, false // "\x1b[m" == reset
	}
	ps := strings.FieldsFunc(body, func(r rune) bool { return r == ';' || r == ':' })
	for i := 0; i < len(ps); i++ {
		switch p := ps[i]; {
		case p == "0" || p == "00":
			touched, set = true, false
		case p == "49":
			touched, set = true, false
		case p == "48":
			touched, set = true, true
			i += extSkip(ps, i)
		case p == "38" || p == "58":
			i += extSkip(ps, i)
		case len(p) == 2 && p[0] == '4' && p[1] >= '0' && p[1] <= '7',
			len(p) == 3 && p[:2] == "10" && p[2] >= '0' && p[2] <= '7':
			touched, set = true, true
		}
	}
	return touched, set
}

// extSkip is how many parameters after an extended-color introducer at
// ps[i] belong to it ("5;n" → 2, "2;r;g;b" → 4).
func extSkip(ps []string, i int) int {
	if i+1 >= len(ps) {
		return 0
	}
	switch ps[i+1] {
	case "5":
		return 2
	case "2":
		return 4
	}
	return 0
}

// Fill paints s on the background sequence bg: bg leads the row and is
// re-emitted after every escape that clears the background (a reset or
// SGR 49), so nested styling can never punch default-colored holes into
// a row (F-064). Escapes that set their own background are left alone.
// The result ends with a reset so nothing leaks past the row.
func Fill(s, bg string) string {
	if bg == "" {
		return s
	}
	var b strings.Builder
	b.WriteString(bg)
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				seq := s[i : i+loc[1]]
				b.WriteString(seq)
				if touched, set := sgrBg(seq); touched && !set {
					b.WriteString(bg)
				}
				i += loc[1]
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += sz
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// Slice returns the cells [from, to) of s with the styling active at
// from replayed first, so a right-hand remainder keeps its colors when a
// box is spliced over the middle of a row (F-064 overlay). A wide rune
// cut by either bound becomes spaces. The result ends with a reset.
func Slice(s string, from, to int) string {
	if to <= from {
		return ""
	}
	var b strings.Builder
	width := 0
	for i := 0; i < len(s) && width < to; {
		if s[i] == 0x1b {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				b.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		switch {
		case width >= from && width+w <= to:
			b.WriteRune(r)
		case width+w > from && width < to:
			// straddles a bound: keep the cells inside as spaces
			lo, hi := max(width, from), min(width+w, to)
			b.WriteString(strings.Repeat(" ", hi-lo))
		}
		width += w
		i += sz
	}
	if width < from {
		return ""
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// Blank replaces the visible text of s with spaces of equal width while
// keeping every escape (so backgrounds stay) and any box-drawing or
// block glyph (so borders and scrollbars stay).
func Blank(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				b.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		if r >= 0x2500 && r <= 0x259F {
			b.WriteRune(r)
		} else {
			b.WriteString(strings.Repeat(" ", runewidth.RuneWidth(r)))
		}
		i += sz
	}
	return b.String()
}

// Cell is one rendered terminal cell: its rune (0 for the right half of
// a wide rune) and whether a non-default background (or reverse video)
// paints it.
type Cell struct {
	R  rune
	Bg bool
}

// Cells renders one line to cells by replaying its SGR state; tests use
// it to check backgrounds, which ANSI-stripped goldens cannot see.
func Cells(s string) []Cell {
	var out []Cell
	bg, rev := false, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				seq := s[i : i+loc[1]]
				if touched, set := sgrBg(seq); touched {
					bg = set
				}
				rev = sgrReverse(seq, rev)
				i += loc[1]
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		out = append(out, Cell{R: r, Bg: bg || rev})
		for k := 1; k < w; k++ {
			out = append(out, Cell{Bg: bg || rev})
		}
		i += sz
	}
	return out
}

// sgrReverse tracks reverse video (7 on; 0/27 or a bare reset off).
func sgrReverse(seq string, cur bool) bool {
	if len(seq) < 3 || seq[len(seq)-1] != 'm' || seq[1] != '[' {
		return cur
	}
	body := seq[2 : len(seq)-1]
	if body == "" {
		return false
	}
	ps := strings.FieldsFunc(body, func(r rune) bool { return r == ';' || r == ':' })
	for i := 0; i < len(ps); i++ {
		switch ps[i] {
		case "0":
			cur = false
		case "7":
			cur = true
		case "27":
			cur = false
		case "38", "48", "58":
			i += extSkip(ps, i)
		}
	}
	return cur
}

// Clip returns the longest prefix of s whose visible width is at most n
// cells, keeping escape sequences intact so styling survives truncation.
func Clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if loc := re.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				b.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		if width+w > n {
			break
		}
		b.WriteRune(r)
		width += w
		i += sz
	}
	return b.String()
}
