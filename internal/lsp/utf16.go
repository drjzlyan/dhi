package lsp

import "unicode/utf8"

// LSP positions count UTF-16 code units (the default position encoding);
// DHI's buffer counts runes. These convert on one line's text (F-116):
// with rune columns, any line with a non-BMP character (emoji) put edits
// at the wrong place.

// UTF16Col converts a rune column on line to UTF-16 units.
func UTF16Col(line string, runeCol int) int {
	n := 0
	for i, r := range []rune(line) {
		if i >= runeCol {
			break
		}
		n += utf16Len(r)
	}
	if extra := runeCol - utf8.RuneCountInString(line); extra > 0 {
		n += extra // past the end: keep the overshoot
	}
	return n
}

// RuneCol converts a UTF-16 column on line to a rune column (clamped to
// the line's end).
func RuneCol(line string, u16 int) int {
	n, col := 0, 0
	for _, r := range line {
		if n >= u16 {
			return col
		}
		n += utf16Len(r)
		col++
	}
	return col
}

func utf16Len(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}
