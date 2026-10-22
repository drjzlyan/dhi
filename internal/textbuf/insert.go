package textbuf

import "strings"

// keyInsert handles insert-mode keys.
func (e *Editor) keyInsert(key string) {
	switch key {
	case "esc", "ctrl+[":
		e.mode = ModeNormal
		// cursor steps left off the insert point like vim
		if e.buf.Cursor().Col > 0 {
			e.buf.cursor.Col--
			e.buf.wantCol = e.buf.Cursor().Col
		}
	case "enter":
		e.buf.BreakLine()
	case "backspace":
		e.buf.DeleteBackspace()
	case "delete":
		e.buf.DeleteForward()
	case "tab":
		e.buf.InsertString("\t")
	case "left":
		e.buf.move(motLeft, 1)
	case "right":
		e.buf.move(motRight, 1)
	case "up":
		e.buf.move(motUp, 1)
	case "down":
		e.buf.move(motDown, 1)
	case "home", "ctrl+a":
		e.buf.move(motLineStart, 1)
	case "end", "ctrl+e":
		e.buf.move(motLineEnd, 1)
		e.buf.cursor.Col = len(e.buf.runes(e.buf.Cursor().Line)) // after the last rune
	case "alt+left", "ctrl+left":
		e.buf.move(motWordBack, 1)
	case "alt+right", "ctrl+right":
		e.buf.move(motWordFwd, 1)
	case "ctrl+w", "alt+backspace":
		c := e.buf.Cursor()
		start := Pos{Line: c.Line, Col: len([]rune(dropWordBack(string(e.buf.runes(c.Line)[:c.Col]))))}
		if start != c {
			e.buf.deleteRange(start, c)
			e.buf.cursor = start
		} else {
			e.buf.DeleteBackspace() // at a line start: join with the line above
		}
	case "ctrl+u":
		c := e.buf.Cursor()
		if c.Col > 0 {
			start := Pos{Line: c.Line}
			e.buf.deleteRange(start, c)
			e.buf.cursor = start
		}
	default:
		if text, ok := strings.CutPrefix(key, "paste:"); ok {
			e.buf.InsertString(strings.ReplaceAll(text, "\r\n", "\n"))
			return
		}
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			e.buf.InsertString(key)
		}
	}
}

// dropWordBack removes the word before the end of s (and the spaces
// after it), like ctrl+w in a shell.
func dropWordBack(s string) string {
	rs := []rune(s)
	i := len(rs)
	for i > 0 && isSpace(rs[i-1]) {
		i--
	}
	for i > 0 && !isSpace(rs[i-1]) {
		i--
	}
	return string(rs[:i])
}
