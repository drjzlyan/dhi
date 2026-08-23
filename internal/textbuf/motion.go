package textbuf

// motion identifies a movement the modal layer can request.
type motion uint8

const (
	motLeft motion = iota
	motRight
	motUp
	motDown
	motLineStart // 0
	motLineEnd   // $
	motWordFwd   // w
	motWordBack  // b
	motWordEnd   // e
	motBOF       // gg
	motEOF       // G
)

// linewise reports whether the motion spans whole lines (affects d/c/y).
func (m motion) linewise() bool {
	switch m {
	case motUp, motDown, motBOF, motEOF:
		return true
	}
	return false
}

// move applies m as a cursor movement repeated count times.
func (b *Buffer) move(m motion, count int) {
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		b.stepOnce(m)
	}
	b.clampCursor()
}

func (b *Buffer) stepOnce(m motion) {
	line := b.runes(b.cursor.Line)
	switch m {
	case motLeft:
		if b.cursor.Col > 0 {
			b.cursor.Col--
			b.wantCol = b.cursor.Col
		} else if b.cursor.Line > 0 { // wrap like vim
			b.cursor.Line--
			b.cursor.Col = len(b.runes(b.cursor.Line))
			b.wantCol = b.cursor.Col
		}
	case motRight:
		if b.cursor.Col < len(line) {
			b.cursor.Col++
			b.wantCol = b.cursor.Col
		} else if b.cursor.Line < len(b.lines)-1 {
			b.cursor.Line++
			b.cursor.Col = 0
			b.wantCol = 0
		}
	case motUp:
		if b.cursor.Line > 0 {
			b.cursor.Line--
			b.applyWantCol()
		}
	case motDown:
		if b.cursor.Line < len(b.lines)-1 {
			b.cursor.Line++
			b.applyWantCol()
		}
	case motLineStart:
		b.cursor.Col = 0
		b.wantCol = 0
	case motLineEnd:
		b.cursor.Col = len(line)
		b.wantCol = b.cursor.Col
	case motBOF:
		b.cursor = Pos{Line: 0, Col: 0}
		b.wantCol = 0
	case motEOF:
		last := len(b.lines) - 1
		b.cursor = Pos{Line: last, Col: len(b.runes(last))}
		b.wantCol = b.cursor.Col
	default:
		b.wordStep(m)
	}
}

// wordStep implements w/b/e semantics on blank-separated words.
func (b *Buffer) wordStep(m motion) {
	switch m {
	case motWordFwd:
		line := b.runes(b.cursor.Line)
		i := b.cursor.Col
		for i < len(line) && !isSpace(line[i]) {
			i++
		}
		for i < len(line) && isSpace(line[i]) {
			i++
		}
		if i < len(line) {
			b.cursor.Col = i
			b.wantCol = i
			return
		}
		if b.cursor.Line < len(b.lines)-1 {
			b.cursor.Line++
			next := b.runes(b.cursor.Line)
			j := 0
			for j < len(next) && isSpace(next[j]) {
				j++
			}
			b.cursor.Col = j
			b.wantCol = j
		} else {
			b.cursor.Col = len(line)
			b.wantCol = len(line)
		}
	case motWordBack:
		line := b.runes(b.cursor.Line)
		i := b.cursor.Col
		if i > 0 && i <= len(line) {
			i--
		}
		for i > 0 && isSpace(line[i]) {
			i--
		}
		for i > 0 && !isSpace(line[i-1]) {
			i--
		}
		b.cursor.Col = i
		b.wantCol = i
	case motWordEnd:
		line := b.runes(b.cursor.Line)
		i := b.cursor.Col
		if i < len(line) {
			i++
		}
		for i < len(line) && isSpace(line[i]) {
			i++
		}
		for i < len(line)-1 && !isSpace(line[i+1]) {
			i++
		}
		b.cursor.Col = minInt(i, len(line))
		b.wantCol = b.cursor.Col
	}
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// applyWantCol restores the sticky column after a vertical move,
// clamping to the new line's width.
func (b *Buffer) applyWantCol() {
	max := len(b.runes(b.cursor.Line))
	col := b.wantCol
	if col > max {
		col = max
	}
	b.cursor.Col = col
}

// target computes where motion+count lands without moving.
func (b *Buffer) target(m motion, count int) Pos {
	saved := b.cursor
	savedWant := b.wantCol
	b.move(m, count)
	t := b.cursor
	b.cursor = saved
	b.wantCol = savedWant
	return t
}

// spanFor returns the half-open rune range an operator covers when
// applied over m with count. Linewise motions yield full-line ranges
// expressed as {line,0}..{lastLine+1,0}.
func (b *Buffer) spanFor(m motion, count int) (Pos, Pos) {
	from := b.cursor
	to := b.target(m, maxInt(count, 1))

	if m.linewise() {
		first := from.Line
		if to.Line < first {
			first = to.Line
		}
		return Pos{first, 0}, Pos{maxInt(from.Line, to.Line) + 1, 0}
	}

	end := to
	switch m {
	case motWordEnd, motLineEnd:
		end.Col++
		if end.Col > len([]rune(b.lines[end.Line])) {
			end.Col = len([]rune(b.lines[end.Line]))
		}
	}
	if !end.before(from) && end != from {
		return from, end
	}
	a, z := order(from, end)
	if a == z { // zero-span charwise: widen by one where possible
		z.Col++
	}
	return a, z
}

func maxInt(a, b2 int) int {
	if a > b2 {
		return a
	}
	return b2
}

func minInt(a, b2 int) int {
	if a < b2 {
		return a
	}
	return b2
}
