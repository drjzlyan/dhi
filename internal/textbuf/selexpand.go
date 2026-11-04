package textbuf

// Selection expand and shrink (F-167). Expanding turns the cursor (or the
// current visual selection) into the next larger range and enters visual
// mode; shrinking walks back through the ranges expanded from. A language
// server's selectionRange feeds ApplySelection; without one the structural
// ladder below decides, and its rung is named (StructuralExpand's name).

// Before reports whether p is strictly earlier in the buffer than q.
func (p Pos) Before(q Pos) bool { return p.before(q) }

// selFrame is a selection to return to on shrink.
type selFrame struct {
	anchor, cur Pos
	mode        Mode
	line        bool
}

// SelectionBounds is the half-open range the selection covers: the
// visual selection (whole lines for V), or the character under the
// cursor in normal mode. ok is false for a block selection and in the
// other modes.
func (e *Editor) SelectionBounds() (a, z Pos, ok bool) {
	switch {
	case e.mode == ModeNormal:
		c := e.buf.Cursor()
		z = c
		if c.Col < len(e.buf.runes(c.Line)) {
			z.Col++
		}
		return c, z, true
	case e.mode != ModeVisual || e.visualBlock:
		return Pos{}, Pos{}, false
	case e.visualLine:
		lo, hi := order(e.visualStart, e.buf.Cursor())
		return Pos{Line: lo.Line}, Pos{Line: hi.Line, Col: len(e.buf.runes(hi.Line))}, true
	}
	a, z = e.visualSpan()
	return a, z, true
}

// chainLive reports whether the selection is still the one the last
// expand/shrink produced (anything else the user did starts a new chain).
func (e *Editor) chainLive() bool {
	return e.mode == ModeVisual && e.selChain && e.selLast == [2]Pos{e.visualStart, e.buf.Cursor()}
}

// ApplySelection selects exactly [a,z) in charwise visual mode, remembering
// the selection it replaces for ShrinkSelection.
func (e *Editor) ApplySelection(a, z Pos) {
	if !e.chainLive() {
		e.selStack = nil
	}
	e.selStack = append(e.selStack, selFrame{anchor: e.visualStart, cur: e.buf.Cursor(), mode: e.mode, line: e.visualLine})
	e.clearPending()
	e.mode, e.visualLine, e.visualBlock = ModeVisual, false, false
	e.visualStart = a
	e.buf.SetCursor(e.buf.lastIn(a, z))
	e.selLast, e.selChain = [2]Pos{e.visualStart, e.buf.Cursor()}, true
}

// ShrinkSelection returns to the selection before the last expand.
func (e *Editor) ShrinkSelection() bool {
	if !e.chainLive() || len(e.selStack) == 0 {
		return false
	}
	f := e.selStack[len(e.selStack)-1]
	e.selStack = e.selStack[:len(e.selStack)-1]
	e.mode, e.visualLine, e.visualBlock = f.mode, f.line, false
	e.visualStart = f.anchor
	e.buf.SetCursor(f.cur)
	e.selLast, e.selChain = [2]Pos{e.visualStart, e.buf.Cursor()}, f.mode == ModeVisual
	if f.mode != ModeVisual {
		e.selStack = nil
	}
	return true
}

// StructuralExpand selects the next rung of the structural ladder and
// returns its name, or "" when nothing larger exists.
func (e *Editor) StructuralExpand() string {
	a, z, ok := e.SelectionBounds()
	if !ok {
		return ""
	}
	na, nz, name := e.buf.ladder(a, z)
	if name == "" {
		return ""
	}
	e.ApplySelection(na, nz)
	return name
}

// lastIn is the cursor position of the last character of [a,z).
func (b *Buffer) lastIn(a, z Pos) Pos {
	if z.Col > 0 {
		return Pos{z.Line, z.Col - 1}
	}
	if z.Line > a.Line {
		return Pos{z.Line - 1, max(len(b.runes(z.Line-1))-1, 0)}
	}
	return z
}

// offset is the rune index of p from the start of the buffer, counting
// each line break once; it orders candidate ranges by size.
func (b *Buffer) offset(p Pos) int {
	n := 0
	for l := 0; l < p.Line && l < len(b.lines); l++ {
		n += len(b.runes(l)) + 1
	}
	return n + p.Col
}

// ladder finds the smallest range strictly containing [sa,sz) among, in
// the order they break ties: word, inside quotes, quotes, inside
// brackets, brackets (each nesting level), the line's text, the line,
// the paragraph, the buffer. Pairs are found around the selection's start,
// quotes on its line. The name says which rung was taken.
func (b *Buffer) ladder(sa, sz Pos) (Pos, Pos, string) {
	var (
		best     [2]Pos
		bestName string
		bestSize int
	)
	try := func(a, z Pos, name string) {
		if z.Col == 0 && z.Line > a.Line {
			z = Pos{z.Line - 1, len(b.runes(z.Line - 1))} // a selection cannot end on a line break
		}
		if !a.before(z) {
			return // empty
		}
		if sa.before(a) || z.before(sz) || (a == sa && z == sz) {
			return // does not strictly contain the selection
		}
		if size := b.offset(z) - b.offset(a); bestName == "" || size < bestSize {
			best, bestName, bestSize = [2]Pos{a, z}, name, size
		}
	}
	if a, z, ok := b.objWord(sa, true, 1); ok {
		try(a, z, "word")
	}
	for _, q := range "\"'`" {
		if a, z, ok := b.objQuote(sa, q, true); ok {
			try(a, z, "inside quotes")
		}
		if a, z, ok := b.objQuote(sa, q, false); ok {
			try(a, z, "quotes")
		}
	}
	for _, p := range [][2]rune{{'(', ')'}, {'[', ']'}, {'{', '}'}} {
		for n := 1; ; n++ {
			o, c, ok := b.findPair(sa, p[0], p[1], n)
			if !ok {
				break
			}
			try(Pos{o.Line, o.Col + 1}, c, "inside brackets")
			try(o, Pos{c.Line, c.Col + 1}, "brackets")
		}
	}
	end := func(l int) Pos { return Pos{l, len(b.runes(l))} }
	if sa.Line == sz.Line || (sz.Col == 0 && sz.Line == sa.Line+1) {
		r := b.runes(sa.Line)
		lo, hi := 0, len(r)
		for lo < hi && runeClass(r[lo]) == clsSpace {
			lo++
		}
		for hi > lo && runeClass(r[hi-1]) == clsSpace {
			hi--
		}
		try(Pos{sa.Line, lo}, Pos{sa.Line, hi}, "line text")
	}
	last := sz.Line
	if sz.Col == 0 && sz.Line > sa.Line {
		last = sz.Line - 1
	}
	try(Pos{Line: sa.Line}, end(last), "line")
	if l1, l2, ok := b.objParagraph(sa.Line, true, 1); ok && !blankLine(b.lines[sa.Line]) {
		try(Pos{Line: l1}, end(l2), "paragraph")
	}
	try(Pos{}, end(len(b.lines)-1), "buffer")
	return best[0], best[1], bestName
}
