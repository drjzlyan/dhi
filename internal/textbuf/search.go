package textbuf

import (
	"strings"
	"unicode"
)

// In-buffer search (F-102): "/" searches forward, "?" backward, n/N
// repeat, * searches the word under the cursor. The prompt shares the
// command line (cmdPrefix tells ":" from "/" and "?"). A query in lower
// case matches any case (vim's smartcase); a capital makes it exact.

// startSearch opens the search prompt.
func (e *Editor) startSearch(dir int) {
	e.mode = ModeCommand
	e.cmdline = nil
	e.cmdPrefix = '/'
	if dir < 0 {
		e.cmdPrefix = '?'
	}
}

// runSearch records q as the last search and jumps to its next match.
func (e *Editor) runSearch(q string, dir int) {
	e.mode = ModeNormal
	if q == "" {
		q = e.lastSearch // "/" then enter repeats the last search
	}
	if q == "" {
		return
	}
	e.lastSearch, e.lastDir = q, dir
	e.searchNext(dir)
}

// searchNext moves to the next match of the last search in direction dir
// (1 forward, -1 backward), wrapping at the ends.
func (e *Editor) searchNext(dir int) {
	q := e.lastSearch
	if q == "" {
		e.message = "no previous search"
		return
	}
	fold := q == strings.ToLower(q)
	if fold {
		q = strings.ToLower(q)
	}
	n := e.buf.LineCount()
	cur := e.buf.Cursor()
	match := func(line int) []int { // rune columns of every match on line
		text := e.buf.Line(line)
		if fold {
			text = strings.ToLower(text)
		}
		var cols []int
		for off := 0; ; {
			i := strings.Index(text[off:], q)
			if i < 0 {
				return cols
			}
			cols = append(cols, len([]rune(text[:off+i])))
			off += i + max(len(q), 1)
		}
	}
	for step := 0; step <= n; step++ {
		line := ((cur.Line+dir*step)%n + n) % n
		cols := match(line)
		if dir > 0 {
			for _, c := range cols {
				if step > 0 || c > cur.Col {
					e.jumpMatch(line, c, step > 0 && line < cur.Line, "BOTTOM", "TOP")
					return
				}
			}
		} else {
			for i := len(cols) - 1; i >= 0; i-- {
				if c := cols[i]; step > 0 || c < cur.Col {
					e.jumpMatch(line, c, step > 0 && line > cur.Line, "TOP", "BOTTOM")
					return
				}
			}
		}
		if step == n { // back on the cursor line after a full lap
			break
		}
	}
	e.message = "pattern not found: " + e.lastSearch
}

func (e *Editor) jumpMatch(line, col int, wrapped bool, from, to string) {
	e.buf.SetCursor(Pos{Line: line, Col: col})
	e.buf.wantCol = col
	e.message = "/" + e.lastSearch
	if wrapped {
		e.message = "search hit " + from + ", continuing at " + to
	}
}

// wordUnderCursor is the identifier the cursor sits on ("" when none).
func (e *Editor) wordUnderCursor() string {
	rs := e.buf.runes(e.buf.Cursor().Line)
	c := e.buf.Cursor().Col
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	if c >= len(rs) || !isWord(rs[c]) {
		return ""
	}
	a, z := c, c
	for a > 0 && isWord(rs[a-1]) {
		a--
	}
	for z < len(rs) && isWord(rs[z]) {
		z++
	}
	return string(rs[a:z])
}

// LastSearch is the active search text (the view highlights it).
func (e *Editor) LastSearch() string { return e.lastSearch }

// SetPageSize tells the editor how many lines one screen shows, for
// ctrl+d/ctrl+u (half a page) and pgup/pgdown (a page).
func (e *Editor) SetPageSize(n int) { e.page = max(n, 2) }

func (e *Editor) scroll(lines int) {
	if lines < 0 {
		e.buf.move(motUp, -lines)
		return
	}
	e.buf.move(motDown, lines)
}

// TakeYank returns, once, the text of the last yank (y, yy, visual y), so
// the surface can put it on the system clipboard too.
func (e *Editor) TakeYank() (string, bool) {
	if e.yanked == nil {
		return "", false
	}
	t := *e.yanked
	e.yanked = nil
	return t, true
}

// SelectFrom starts (or re-anchors) a visual selection at anchor, the
// cursor marking its other end (a mouse drag, F-103).
func (e *Editor) SelectFrom(anchor Pos) {
	if e.mode == ModeInsert {
		e.buf.EndUndoGroup() // leaving insert closes its undo step
	}
	e.clearPending()
	e.mode = ModeVisual
	e.visualStart = anchor
}
