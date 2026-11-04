package textbuf

import "strings"

// Vim text objects (F-167): iw aw, i( a( i[ a[ i{ a{ (also b and B), i" a"
// i' a' i` a`, ip ap. They are the operand of d/c/y/gc and of visual mode,
// and the pieces of the structural selection ladder (selexpand.go).
// Brackets match across lines; quotes work within one line. Brackets inside
// strings and comments are counted like any other (no syntax awareness).

// textObject resolves one text object around at. The range is half-open;
// linewise ranges (paragraphs) run from {first,0} to {last+1,0}. ok is
// false when there is no such object there. An empty range (i( on "()")
// is ok: c still has a place to insert.
func (b *Buffer) textObject(at Pos, inner bool, obj rune, count int) (a, z Pos, linewise, ok bool) {
	if count < 1 {
		count = 1
	}
	at = b.clampPos(at)
	switch obj {
	case 'w':
		a, z, ok = b.objWord(at, inner, count)
	case '(', ')', 'b':
		a, z, ok = b.objPair(at, '(', ')', inner, count)
	case '[', ']':
		a, z, ok = b.objPair(at, '[', ']', inner, count)
	case '{', '}', 'B':
		a, z, ok = b.objPair(at, '{', '}', inner, count)
	case '"', '\'', '`':
		a, z, ok = b.objQuote(at, obj, inner)
	case 'p':
		l1, l2, found := b.objParagraph(at.Line, inner, count)
		return Pos{Line: l1}, Pos{Line: l2 + 1}, true, found
	}
	return a, z, false, ok
}

const (
	clsSpace = iota
	clsPunct
	clsWord
)

func runeClass(r rune) int {
	switch {
	case r == ' ' || r == '\t':
		return clsSpace
	case r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127:
		return clsWord
	}
	return clsPunct
}

// runEnd is the end of the run of r[i]'s class starting at i.
func runEnd(r []rune, i int) int {
	c := runeClass(r[i])
	for i < len(r) && runeClass(r[i]) == c {
		i++
	}
	return i
}

func (b *Buffer) objWord(at Pos, inner bool, count int) (Pos, Pos, bool) {
	r := b.runes(at.Line)
	if len(r) == 0 {
		return Pos{}, Pos{}, false
	}
	col := min(at.Col, len(r)-1)
	start := col
	for start > 0 && runeClass(r[start-1]) == runeClass(r[col]) {
		start--
	}
	end := runEnd(r, col)
	if inner {
		for i := 1; i < count && end < len(r); i++ {
			end = runEnd(r, end)
		}
		return Pos{at.Line, start}, Pos{at.Line, end}, true
	}
	// aw: a word and the blanks after it; on blanks, the blanks and the
	// word after them. Without trailing blanks the ones before are taken
	// (unless they are the line's indent).
	if runeClass(r[col]) == clsSpace {
		if end < len(r) {
			end = runEnd(r, end)
		}
	} else if end < len(r) && runeClass(r[end]) == clsSpace {
		end = runEnd(r, end)
	} else if count == 1 {
		s := start
		for s > 0 && runeClass(r[s-1]) == clsSpace {
			s--
		}
		if s > 0 {
			start = s
		}
	}
	for i := 1; i < count && end < len(r); i++ {
		if runeClass(r[end]) == clsSpace {
			end = runEnd(r, end)
		}
		if end < len(r) {
			end = runEnd(r, end)
		}
		if end < len(r) && runeClass(r[end]) == clsSpace {
			end = runEnd(r, end)
		}
	}
	return Pos{at.Line, start}, Pos{at.Line, end}, true
}

// findPair locates the count-th innermost open..close pair around at:
// the open and close positions. Standing on a bracket selects that pair.
func (b *Buffer) findPair(at Pos, open, close rune, count int) (o, c Pos, ok bool) {
	depth, found := 0, 0
	for l := at.Line; l >= 0; l-- {
		r := b.runes(l)
		col := len(r) - 1
		if l == at.Line {
			col = min(at.Col, col)
		}
		for ; col >= 0; col-- {
			ch := r[col]
			switch {
			case ch == close && (l != at.Line || col != at.Col):
				depth++
			case ch == open:
				if depth > 0 {
					depth--
					continue
				}
				if found++; found < count {
					continue
				}
				o = Pos{l, col}
				c, ok = b.matchForward(o, open, close)
				return o, c, ok
			}
		}
	}
	return Pos{}, Pos{}, false
}

// matchForward finds the close matching the open at o, across lines.
func (b *Buffer) matchForward(o Pos, open, close rune) (Pos, bool) {
	depth := 0
	for l := o.Line; l < len(b.lines); l++ {
		r := b.runes(l)
		start := 0
		if l == o.Line {
			start = o.Col
		}
		for col := start; col < len(r); col++ {
			switch r[col] {
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return Pos{l, col}, true
				}
			}
		}
	}
	return Pos{}, false
}

func (b *Buffer) objPair(at Pos, open, close rune, inner bool, count int) (Pos, Pos, bool) {
	o, c, ok := b.findPair(at, open, close, count)
	if !ok {
		return Pos{}, Pos{}, false
	}
	if !inner {
		return o, Pos{c.Line, c.Col + 1}, true
	}
	s, e := Pos{o.Line, o.Col + 1}, c
	if c.Line > o.Line {
		// {<newline> ... <indent>}: the lines between, not the line breaks
		// and the closer's indent.
		if o.Col+1 >= len(b.runes(o.Line)) {
			s = Pos{Line: o.Line + 1}
		}
		if blankLine(string(b.runes(c.Line)[:c.Col])) {
			e = Pos{Line: c.Line}
		}
	}
	if e.before(s) {
		e = s
	}
	return s, e, true
}

// quotePair finds the quote pair at col on one line: the pair around the
// cursor, else the next one ahead. Quotes preceded by a backslash don't
// count; quotes pair up in order from the line start.
func quotePair(r []rune, col int, q rune) (qo, qc int, ok bool) {
	var qs []int
	for i, ch := range r {
		if ch != q {
			continue
		}
		bs := 0
		for j := i - 1; j >= 0 && r[j] == '\\'; j-- {
			bs++
		}
		if bs%2 == 0 {
			qs = append(qs, i)
		}
	}
	for i := 0; i+1 < len(qs); i += 2 {
		if col <= qs[i+1] {
			return qs[i], qs[i+1], true
		}
	}
	return 0, 0, false
}

func (b *Buffer) objQuote(at Pos, q rune, inner bool) (Pos, Pos, bool) {
	r := b.runes(at.Line)
	qo, qc, ok := quotePair(r, at.Col, q)
	if !ok {
		return Pos{}, Pos{}, false
	}
	if inner {
		return Pos{at.Line, qo + 1}, Pos{at.Line, qc}, true
	}
	s, e := qo, qc+1
	if e < len(r) && runeClass(r[e]) == clsSpace {
		e = runEnd(r, e)
	} else {
		for s > 0 && runeClass(r[s-1]) == clsSpace && !blankLine(string(r[:s])) {
			s--
		}
	}
	return Pos{at.Line, s}, Pos{at.Line, e}, true
}

func blankLine(s string) bool { return strings.TrimSpace(s) == "" }

// objParagraph is ip/ap over lines: runs of blank or of non-blank lines.
// ap adds the blank run after a paragraph (before it when it ends the
// buffer), or the paragraph after a blank run. Counts repeat the unit.
func (b *Buffer) objParagraph(line int, inner bool, count int) (l1, l2 int, ok bool) {
	last := len(b.lines) - 1
	run := func(from int) int { // last line of the run starting at from
		k := blankLine(b.lines[from])
		for from < last && blankLine(b.lines[from+1]) == k {
			from++
		}
		return from
	}
	l1 = line
	for l1 > 0 && blankLine(b.lines[l1-1]) == blankLine(b.lines[line]) {
		l1--
	}
	l2 = l1 - 1
	for i := 0; i < count; i++ {
		if l2+1 > last {
			break
		}
		l2 = run(l2 + 1)
		if inner {
			continue
		}
		if l2+1 <= last {
			l2 = run(l2 + 1)
		} else if i == 0 && !blankLine(b.lines[l1]) && l1 > 0 {
			l1-- // a last paragraph with no blank after: take the blanks before
			for l1 > 0 && blankLine(b.lines[l1-1]) {
				l1--
			}
		}
	}
	return l1, l2, l2 >= l1
}
