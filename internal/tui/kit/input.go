package kit

import (
	"strings"
	"unicode/utf8"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Input is a text field drawn as a control (F-096): a well in the input
// color behind a left edge that takes the focus color, the value with a
// cursor block when focused, and a muted placeholder when empty. A
// standalone Input may add a help row and an error row under the well;
// Form draws its rows through inputRow and keeps one shared hint line.
//
// Multiline inputs hold "\n" in Value; ctrl+j inserts one and the well
// grows a row per line.
type Input struct {
	Value       string
	Placeholder string
	Help        string // muted line under the well
	Err         string // shown under the well in the danger color, replacing Help
	Focused     bool
	Secret      bool
	Multiline   bool
	cur         int // rune index into Value
	undo, redo  []inputSnap
}

// NewInput builds an input pre-filled with value, cursor at the end.
func NewInput(value, placeholder string) Input {
	return Input{Value: value, Placeholder: placeholder, cur: utf8.RuneCountInString(value)}
}

// Cursor is the rune index of the cursor in Value.
func (in Input) Cursor() int { return min(max(in.cur, 0), utf8.RuneCountInString(in.Value)) }

// SetValue replaces the value and moves the cursor to its end.
func (in *Input) SetValue(v string) { in.Value, in.cur = v, utf8.RuneCountInString(v) }

// HandleKey edits the value through EditText (F-102); ctrl+z undoes and
// ctrl+shift+z redoes. It reports whether the key was an edit or cursor
// move (other keys are left for the caller: enter, tab, esc).
func (in *Input) HandleKey(key string) bool {
	switch key {
	case "ctrl+z":
		in.restore(&in.undo, &in.redo)
		return true
	case "ctrl+shift+z":
		in.restore(&in.redo, &in.undo)
		return true
	}
	before := inputSnap{in.Value, in.Cursor()}
	v, cur, ok := EditText(in.Value, in.Cursor(), key, in.Multiline)
	if !ok {
		return false
	}
	if v != in.Value {
		in.undo = append(in.undo, before)
		if len(in.undo) > maxUndo {
			in.undo = in.undo[1:]
		}
		in.redo = nil
	}
	in.Value, in.cur = v, cur
	return true
}

// Paste inserts text at the cursor as one undoable edit.
func (in *Input) Paste(text string) { in.HandleKey("paste:" + text) }

// WithCursor renders the value with glyph at the cursor, for callers that
// draw their own row (composers, filters, the palette query).
func (in Input) WithCursor(glyph string) string { return CursorText(in.Value, in.Cursor(), glyph) }

// Clear empties the value (an undoable edit).
func (in *Input) Clear() {
	if in.Value != "" {
		in.undo = append(in.undo, inputSnap{in.Value, in.Cursor()})
	}
	in.Value, in.cur = "", 0
}

const maxUndo = 200

type inputSnap struct {
	value string
	cur   int
}

func (in *Input) restore(from, to *[]inputSnap) {
	if len(*from) == 0 {
		return
	}
	s := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, inputSnap{in.Value, in.Cursor()})
	in.Value, in.cur = s.value, s.cur
}

func lineStart(rs []rune, cur int) int {
	for cur > 0 && rs[cur-1] != '\n' {
		cur--
	}
	return cur
}

func lineEnd(rs []rune, cur int) int {
	for cur < len(rs) && rs[cur] != '\n' {
		cur++
	}
	return cur
}

// View renders the well at width w (at least 4 cells), then the error or
// help row when set.
func (in Input) View(w int) []string {
	w = max(w, 4)
	var out []string
	if in.Value == "" && !in.Focused {
		out = append(out, inputRow("", in.Placeholder, -1, false, w))
	} else {
		value := in.Value
		if in.Secret {
			value = strings.Repeat("•", utf8.RuneCountInString(value))
		}
		lines := strings.Split(value, "\n")
		cur := in.Cursor()
		for _, l := range lines {
			n := utf8.RuneCountInString(l)
			c := -1
			if in.Focused && cur >= 0 && cur <= n {
				c = cur
			}
			if in.Multiline {
				out = append(out, wrapRows(l, c, in.Focused, w)...)
			} else {
				out = append(out, inputRow(l, "", c, in.Focused, w))
			}
			cur -= n + 1
		}
	}
	switch {
	case in.Err != "":
		out = append(out, theme.DangerText().Render(ClipEllipsis(" "+theme.GlyphCross+" "+in.Err, w)))
	case in.Help != "":
		for _, l := range WrapWords(in.Help, w-1) {
			out = append(out, theme.TextMuted().Render(" "+l))
		}
	}
	return out
}

// inputRow draws one row of a well: the edge, then text (or the
// placeholder) padded to w. cur >= 0 draws the cursor block at that rune
// index and scrolls a long value so the cursor stays in view.
func inputRow(text, placeholder string, cur int, focused bool, w int) string {
	avail := max(w-2, 1) // edge + one cell of padding
	body := ""
	switch {
	case text == "" && cur < 0 && placeholder != "":
		ph := ClipEllipsis(placeholder, avail)
		return theme.InputEdge(focused).Render(theme.GlyphGutterBar) +
			theme.Placeholder().Render(" "+ph+strings.Repeat(" ", max(avail-runeWidth(ph), 0)))
	case cur >= 0:
		rs := []rune(text)
		start := 0
		if cur >= avail-1 {
			start = cur - avail + 2
		}
		end := min(len(rs), start+avail-1)
		body = string(rs[start:cur]) + theme.GlyphCursor + string(rs[cur:max(cur, end)])
		if start > 0 {
			body = "…" + string([]rune(body)[1:])
		}
	default:
		body = ClipEllipsis(text, avail)
	}
	body = ClipEllipsis(body, avail)
	return theme.InputEdge(focused).Render(theme.GlyphGutterBar) +
		theme.Input().Render(" "+body+strings.Repeat(" ", max(avail-runeWidth(body), 0)))
}

// wrapRows soft-wraps one line of a multi-line input into well rows of
// w cells (character wrap keeps the cursor exact); c >= 0 is the cursor's
// rune index in the line.
func wrapRows(line string, c int, focused bool, w int) []string {
	cw := max(w-3, 1) // edge, padding, room for the cursor block
	rs := []rune(line)
	var out []string
	for start := 0; ; start += cw {
		end := min(start+cw, len(rs))
		rc := -1
		if c >= start && (c < end || (c == end && end-start < cw)) {
			rc = c - start
		}
		out = append(out, inputRow(string(rs[start:end]), "", rc, focused, w))
		if end >= len(rs) && (c != len(rs) || end-start < cw || len(rs) == 0) {
			return out
		}
		if end >= len(rs) {
			// The cursor sits after a full last row: give it its own row.
			return append(out, inputRow("", "", 0, focused, w))
		}
	}
}
