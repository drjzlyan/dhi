package kit

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// StatusSegment is one styled chunk of the statusline.
type StatusSegment struct {
	Text  string
	Style lipgloss.Style // zero value renders plain
}

// StatusLine is the one row that owns every hint (ADR-0040): the view's
// mode block, zone and attention on the left, then the flash, and on the
// right the active surface's keymap followed by the global keys.
//
// Overflow never clips a hint mid-word: whole hints drop, globals first
// (the first global — the palette — survives longest), then the
// surface's hints from the least important end, and More ("? more")
// takes their place so the dropped keys stay one keystroke away.
type StatusLine struct {
	Left    []StatusSegment
	Flash   string   // pre-rendered outcome (✓ saved, ✗ failed); dropped last on the right
	Hints   []string // the surface's keymap, most important first
	Globals []string // shell keys, most important first
	More    string   // shown when a surface hint was dropped ("" = never, e.g. while typing)
	Center  string   // legacy middle segment; dropped first
	Version string
	Width   int

	spans []hintSpan // the last View's clickable hints
}

// hintSpan is one rendered hint's columns, for click routing: the
// logical hint (before F-058 display remaps) and whether it is a shell
// key or the surface's own.
type hintSpan struct {
	from, to int
	hint     string
	global   bool
}

// keyEntry is one hint queued for rendering.
type keyEntry struct {
	hint   string // logical; rendered through DisplayHint
	global bool
}

// DefaultStatusLine returns the neutral base statusline for the given
// surface name: the view's mode block on the left, the global keys on
// the right.
func DefaultStatusLine(surfaceName string) *StatusLine {
	return &StatusLine{
		Left:    []StatusSegment{ModeBlock(surfaceName)},
		Globals: []string{"^p palette", "? help", "1-5 views", "tab next", "^c quit"},
		More:    "? more",
	}
}

// ModeBlock renders the view's name as a solid block in its hue — the
// first thing on the status row, so a glance says where you are.
func ModeBlock(name string) StatusSegment {
	return StatusSegment{
		Text:  " " + strings.ToUpper(name) + " ",
		Style: theme.ModeBlock(),
	}
}

// ModeChip renders a statusline mode segment as a pill in the active
// view's accent (INSERT / FIND / CHAT …).
func ModeChip(text string) StatusSegment {
	return StatusSegment{
		Text:  " " + strings.ToUpper(text) + " ",
		Style: theme.Pill(theme.SurfaceAccent()).Bold(true), // the focus hue (F-064)
	}
}

// renderKeys renders hints as keycap + muted label, two cells apart,
// and reports each hint's column span (relative to the first cell).
func renderKeys(entries []keyEntry) (string, []hintSpan) {
	hint := theme.Hint()
	var out string
	var spans []hintSpan
	for i, e := range entries {
		h := DisplayHint(e.hint) // name the key to press (F-058 remaps)
		if i > 0 {
			out += "  "
		}
		from := runeWidth(out)
		key, desc := h, ""
		if j := strings.Index(h, " "); j > 0 {
			key, desc = h[:j], h[j+1:]
		}
		out += theme.Keycap().Render(" " + key + " ")
		if desc != "" {
			out += hint.Render(" " + desc)
		}
		spans = append(spans, hintSpan{from: from, to: runeWidth(out), hint: e.hint, global: e.global})
	}
	return out, spans
}

// View renders the statusline padded to Width cells when Width > 0.
func (s *StatusLine) View() string {
	var left string
	for _, seg := range s.Left {
		left += seg.Style.Render(seg.Text)
	}
	flash := s.Flash

	hints := append([]string{}, s.Hints...)
	globals := append([]string{}, s.Globals...)
	dropped := false

	var spans []hintSpan
	right := func() string {
		all := make([]keyEntry, 0, len(hints)+len(globals)+1)
		for _, h := range hints {
			all = append(all, keyEntry{hint: h})
		}
		if dropped && s.More != "" {
			all = append(all, keyEntry{hint: s.More, global: true})
		}
		for _, g := range globals {
			if dropped && s.More != "" && strings.HasPrefix(g, "? ") {
				continue // "? more" already offers help
			}
			all = append(all, keyEntry{hint: g, global: true})
		}
		out, sp := renderKeys(all)
		spans = sp
		return out
	}
	center := s.Center
	width := func() int {
		w := runeWidth(left) + runeWidth(center) + runeWidth(right()) + 2
		if flash != "" {
			w += runeWidth(flash) + 2
		}
		return w
	}
	fits := func() bool { return s.Width <= 0 || width() <= s.Width }

	if !fits() {
		center = ""
	}
	for !fits() && len(globals) > 1 {
		globals = globals[:len(globals)-1]
	}
	for !fits() && len(hints) > 0 {
		hints = hints[:len(hints)-1]
		dropped = true
	}
	for !fits() && len(globals) > 0 {
		globals = globals[:len(globals)-1]
	}
	if !fits() && s.More != "" {
		dropped = false
		hints = nil
	}
	if !fits() {
		flash = ""
	}
	r := right()
	if !fits() {
		// Tiny terminals: the left side yields, clipped visibly.
		r, spans = "", nil
		left = ClipEllipsis(left, s.Width)
	}

	out := left
	if flash != "" {
		out += "  " + flash
	}
	if center != "" {
		out += "  " + center
	}
	if s.Width <= 0 {
		s.setSpans(spans, runeWidth(out)+2)
		return out + "  " + r
	}
	gap := s.Width - runeWidth(out) - runeWidth(r) - 1
	if gap < 1 {
		gap = 1
	}
	out += strings.Repeat(" ", gap)
	s.setSpans(spans, runeWidth(out))
	out += r
	return PaintRow(out, s.Width, theme.StatusBar()) // one background end to end (F-064)
}

// KeyAt maps a click at column x of the last View to the logical key of
// the hint drawn there (F-055: the keymap is clickable): the first
// alternative of the hint's key part ("s/S status" → "s"), and whether
// it is a shell key (global) or the surface's. False off a hint, or
// before View ran.
func (s *StatusLine) KeyAt(x int) (key string, global, ok bool) {
	for _, sp := range s.spans {
		if x >= sp.from && x < sp.to {
			rows := HintRows(sp.hint)
			if len(rows) == 0 {
				return "", false, false
			}
			k := strings.Fields(strings.ReplaceAll(rows[0][0], "/", " "))[0]
			if k == "space" {
				k = " "
			}
			return k, sp.global, true
		}
	}
	return "", false, false
}

// setSpans records the rendered hints' absolute columns.
func (s *StatusLine) setSpans(spans []hintSpan, offset int) {
	s.spans = s.spans[:0]
	for _, sp := range spans {
		s.spans = append(s.spans, hintSpan{from: sp.from + offset, to: sp.to + offset, hint: sp.hint, global: sp.global})
	}
}
