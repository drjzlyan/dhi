package kit

import (
	"strings"
	"unicode"
)

// Line editing (F-102). Every text field in DHI — form fields, composers,
// filters, the palette query, find and rename boxes — edits through
// EditText, so each one has the same cursor, word motion and kill keys a
// shell prompt has, and handles runes (never bytes).
//
// Keys:
//   - left/right; alt+left/right and ctrl+left/right move by word
//   - home/ctrl+a and end/ctrl+e (to the start or end of the line)
//   - backspace, delete/ctrl+d
//   - alt+backspace and ctrl+w delete the word before the cursor;
//     alt+delete and alt+d the word after it
//   - ctrl+u kills to the start of the line, ctrl+k to its end
//   - ctrl+j inserts a newline when multiline
//   - "paste:<text>" inserts text (a single-line field joins its lines)
//   - any printable rune inserts itself
//
// Callers check their own keys (enter, esc, tab, list navigation) first
// and pass the rest here.

// EditText applies key to value with the cursor at rune index cur. It
// returns the new value and cursor, and whether key was an edit or a
// cursor move.
func EditText(value string, cur int, key string, multiline bool) (string, int, bool) {
	rs := []rune(value)
	cur = min(max(cur, 0), len(rs))
	switch key {
	case "left":
		return value, max(cur-1, 0), true
	case "right":
		return value, min(cur+1, len(rs)), true
	case "alt+left", "ctrl+left":
		return value, wordLeft(rs, cur), true
	case "alt+right", "ctrl+right":
		return value, wordRight(rs, cur), true
	case "home", "ctrl+a":
		return value, lineStart(rs, cur), true
	case "end", "ctrl+e":
		return value, lineEnd(rs, cur), true
	case "backspace", "ctrl+h":
		if cur == 0 {
			return value, cur, true
		}
		return cut(rs, cur-1, cur), cur - 1, true
	case "delete", "ctrl+d":
		if cur == len(rs) {
			return value, cur, true
		}
		return cut(rs, cur, cur+1), cur, true
	case "alt+backspace", "ctrl+w":
		from := wordLeft(rs, cur)
		return cut(rs, from, cur), from, true
	case "alt+delete", "alt+d":
		return cut(rs, cur, wordRight(rs, cur)), cur, true
	case "ctrl+u":
		from := lineStart(rs, cur)
		return cut(rs, from, cur), from, true
	case "ctrl+k":
		return cut(rs, cur, lineEnd(rs, cur)), cur, true
	case "ctrl+j":
		if !multiline {
			return value, cur, false
		}
		return insertAt(rs, cur, []rune{'\n'})
	}
	if text, ok := strings.CutPrefix(key, "paste:"); ok {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		if !multiline {
			// A copied token usually carries a trailing newline; inner
			// line breaks become spaces.
			text = strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", " ")
		}
		return insertAt(rs, cur, printable([]rune(text), multiline))
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 && r[0] != 127 {
		return insertAt(rs, cur, r)
	}
	return value, cur, false
}

// CursorText renders value with the cursor glyph at rune index cur, for
// fields that draw their own row.
func CursorText(value string, cur int, glyph string) string {
	rs := []rune(value)
	cur = min(max(cur, 0), len(rs))
	return string(rs[:cur]) + glyph + string(rs[cur:])
}

func insertAt(rs []rune, cur int, add []rune) (string, int, bool) {
	out := make([]rune, 0, len(rs)+len(add))
	out = append(out, rs[:cur]...)
	out = append(out, add...)
	out = append(out, rs[cur:]...)
	return string(out), cur + len(add), true
}

func cut(rs []rune, from, to int) string {
	return string(rs[:from]) + string(rs[to:])
}

// printable keeps runes a field can show (tabs become spaces).
func printable(rs []rune, multiline bool) []rune {
	out := rs[:0:0]
	for _, r := range rs {
		switch {
		case r == '\t':
			out = append(out, ' ')
		case r == '\n' && multiline:
			out = append(out, r)
		case r >= 32 && r != 127:
			out = append(out, r)
		}
	}
	return out
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// wordLeft skips non-word runes, then word runes, backwards.
func wordLeft(rs []rune, cur int) int {
	for cur > 0 && !isWordRune(rs[cur-1]) {
		cur--
	}
	for cur > 0 && isWordRune(rs[cur-1]) {
		cur--
	}
	return cur
}

// wordRight skips non-word runes, then word runes, forwards.
func wordRight(rs []rune, cur int) int {
	for cur < len(rs) && !isWordRune(rs[cur]) {
		cur++
	}
	for cur < len(rs) && isWordRune(rs[cur]) {
		cur++
	}
	return cur
}

// EditQuery edits a filter or search query whose cursor stays at the end
// (palette, help, symbols, rail and board filters): rune-safe backspace,
// ctrl+w / alt+backspace / ctrl+u, paste and typed runes. It reports
// whether key changed or belonged to the query.
func EditQuery(q, key string) (string, bool) {
	if key == "space" {
		key = " "
	}
	switch key {
	case "left", "right", "home", "end", "ctrl+a", "ctrl+e", "ctrl+k", "alt+left", "alt+right":
		return q, false // a query has no cursor; these stay the caller's
	}
	v, _, ok := EditText(q, len([]rune(q)), key, false)
	return v, ok
}

// EditRunes is EditQuery for callers that hold their text as runes.
func EditRunes(rs []rune, key string) ([]rune, bool) {
	q, ok := EditQuery(string(rs), key)
	if !ok {
		return rs, false
	}
	return []rune(q), true
}
