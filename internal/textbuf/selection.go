package textbuf

import "strings"

// Selection returns the text covered by the active visual selection
// (half-open, including the character under the cursor like real visual
// mode) and its bounds. ok is false outside visual mode.
func (e *Editor) Selection() (text string, from, to Pos, ok bool) {
	if e.mode != ModeVisual {
		return "", Pos{}, Pos{}, false
	}
	a, z := e.visualSpan()
	if a.Line == z.Line {
		r := e.buf.runes(a.Line)
		lo, hi := clampCol(a.Col, len(r)), clampCol(z.Col, len(r))
		return string(r[lo:hi]), a, z, true
	}
	var b strings.Builder
	first := e.buf.runes(a.Line)
	b.WriteString(string(first[clampCol(a.Col, len(first)):]))
	for l := a.Line + 1; l < z.Line; l++ {
		b.WriteByte('\n')
		b.WriteString(e.buf.lines[l])
	}
	last := e.buf.runes(z.Line)
	b.WriteByte('\n')
	b.WriteString(string(last[:clampCol(z.Col, len(last))]))
	return b.String(), a, z, true
}

// clampCol bounds a column to [0, hi].
func clampCol(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

// Selected is a captured visual selection.
type Selected struct {
	Text     string
	From, To Pos
}

// TakeSelection returns the selection captured when ":" left visual mode
// and clears it, so a later command line cannot reuse a stale one.
func (e *Editor) TakeSelection() (Selected, bool) {
	if e.lastSel == nil {
		return Selected{}, false
	}
	s := *e.lastSel
	e.lastSel = nil
	return s, true
}
