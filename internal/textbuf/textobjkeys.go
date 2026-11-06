package textbuf

// finishObject completes d/c/y/gc + i/a + object in normal mode (F-167).
func (e *Editor) finishObject(key string) {
	inner := e.pendObj == 'i'
	e.pendObj = 0
	r := []rune(key)
	if len(r) != 1 || e.pendingOp == opNone {
		e.clearPending()
		return
	}
	count := e.count() * max(e.pendingCnt, 1)
	a, z, lw, ok := e.buf.textObject(e.buf.cursor, inner, r[0], count)
	if !ok {
		e.message = "no such text object here"
		e.clearPending()
		return
	}
	op := e.pendingOp
	e.clearPending()
	if a == z { // i( on "()": nothing to take, but c has a place to type
		if op == opChange {
			e.buf.SetCursor(a)
			e.mode = ModeInsert
		}
		return
	}
	e.runOperator(op, a, z, lw)
}

// visualObject completes i/a + object in visual mode: it selects the
// object around the cursor. When the selection already covers it, the
// next larger one is taken (vi( twice is the enclosing parens).
func (e *Editor) visualObject(key string) {
	inner := e.pendObj == 'i'
	e.pendObj = 0
	r := []rune(key)
	count := e.count()
	e.cntDigits = nil
	if len(r) != 1 {
		return
	}
	sa, sz, _ := e.SelectionBounds()
	// A bare "v" selection takes the first object; one an object made
	// (even a single character) is grown instead.
	pair := [2]Pos{e.visualStart, e.buf.Cursor()}
	trivial := pair[0] == pair[1] && (!e.objSel || e.objLast != pair)
	defer func() { e.objLast, e.objSel = [2]Pos{e.visualStart, e.buf.Cursor()}, e.mode == ModeVisual }()
	for n := count; n <= count+32; n++ {
		a, z, lw, ok := e.buf.textObject(e.buf.cursor, inner, r[0], n)
		if !ok {
			e.message = "no such text object here"
			return
		}
		if a == z {
			return
		}
		if !trivial && (sa.before(a) || z.before(sz) || (a == sa && z == sz)) {
			continue // the selection already covers it: try the next level out
		}
		if lw {
			e.visualLine, e.visualStart = true, a
			e.buf.SetCursor(Pos{Line: z.Line - 1})
			return
		}
		e.visualLine = false
		e.visualStart = a
		e.buf.SetCursor(e.buf.lastIn(a, z))
		return
	}
}
