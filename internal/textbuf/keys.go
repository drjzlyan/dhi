package textbuf

import (
	"strconv"
	"strings"
)

// Key feeds one keystroke (bubbletea msg.String() style: "a", "enter",
// "esc", "backspace", "up", "down", …). It is the entire modal surface.
func (e *Editor) Key(key string) {
	defer e.foldAfterKey()
	if isFoldKey(key) && e.foldKey(key) {
		return // z commands run once, never per cursor and never as the last change (F-176)
	}
	if key == "." && e.mode == ModeNormal && e.pendingOp == opNone && e.pendShift == "" && e.pendFind == 0 && !e.replaying {
		e.repeat()
		return
	}
	if (e.mc != nil || isMCKey(key)) && e.mcKey(key) {
		return // add / skip / collapse a cursor (F-174)
	}
	e.record(key)
	was := e.mode
	if e.mc != nil && !e.inMulti {
		e.keyMulti(key)
	} else {
		e.KeyUngrouped(key)
	}
	e.recordDone()
	// Insert sessions undo as one step: open the group on entering
	// insert mode, close it on leaving.
	if e.mode == ModeInsert && was != ModeInsert {
		if e.preGrouped {
			e.preGrouped = false // a block c/I/A opened it already
		} else {
			e.buf.BeginUndoGroup()
		}
	}
	if was == ModeInsert && e.mode != ModeInsert {
		if e.blockIns != nil {
			e.finishBlockInsert()
		}
		e.buf.EndUndoGroup()
	}
}

// KeyUngrouped is Key without insert-session grouping.
func (e *Editor) KeyUngrouped(key string) {
	switch e.mode {
	case ModeInsert:
		e.keyInsert(key)
	case ModeVisual:
		e.keyVisual(key)
	case ModeCommand:
		e.keyCommand(key)
	default:
		e.keyNormal(key)
	}
}

// handlePendFind processes the character input for f/F/t/T motions.
// Returns true if handled, false to continue normal processing.
func (e *Editor) handlePendFind(key string) bool {
	if e.pendFind == 0 {
		return false
	}
	// Consume the key and store it
	var ch rune
	if len(key) == 1 {
		ch = []rune(key)[0]
	} else if key == "space" {
		ch = ' '
	} else {
		// Multi-char or special key: cancel the find
		e.pendFind = 0
		return false
	}

	e.lastFind.kind = e.pendFind
	e.lastFind.ch = ch
	cnt := e.count()

	// If in operator-pending mode, compute the span and run the operator
	if e.pendingOp != opNone {
		col := e.findCharCol(e.pendFind, ch, cnt)
		if col < 0 {
			// Character not found: cancel the operator
			e.clearPending()
			e.pendFind = 0
			return true
		}
		a, z := e.buf.spanForFind(e.pendFind, col)
		linewise := false
		e.runOperator(e.pendingOp, a, z, linewise)
		e.clearPending()
		e.pendFind = 0
		return true
	}

	// Otherwise just move the cursor
	e.execFind(e.pendFind, ch, cnt)
	e.cntDigits = nil // clear count after using it
	e.pendFind = 0
	return true
}

// reverseFind flips a find's direction for "," (f↔F, t↔T).
func reverseFind(kind rune) rune {
	switch kind {
	case 'f':
		return 'F'
	case 'F':
		return 'f'
	case 't':
		return 'T'
	case 'T':
		return 't'
	}
	return kind
}

// findCharCol finds the column of the target character in the current line.
func (e *Editor) findCharCol(kind rune, ch rune, count int) int {
	if count < 1 {
		count = 1
	}
	switch kind {
	case 'f':
		return e.buf.findChar(ch, count, true)
	case 'F':
		return e.buf.findChar(ch, count, false)
	case 't':
		return e.buf.untilChar(ch, count, true)
	case 'T':
		return e.buf.untilChar(ch, count, false)
	}
	return -1
}

// execFind executes a find/until motion (f/F/t/T) with the given character.
func (e *Editor) execFind(kind rune, ch rune, count int) {
	if count < 1 {
		count = 1
	}
	switch kind {
	case 'f':
		col := e.buf.findChar(ch, count, true)
		if col >= 0 {
			e.buf.cursor.Col = col
			e.buf.wantCol = col
		}
	case 'F':
		col := e.buf.findChar(ch, count, false)
		if col >= 0 {
			e.buf.cursor.Col = col
			e.buf.wantCol = col
		}
	case 't':
		col := e.buf.untilChar(ch, count, true)
		if col >= 0 {
			e.buf.cursor.Col = col
			e.buf.wantCol = col
		}
	case 'T':
		col := e.buf.untilChar(ch, count, false)
		if col >= 0 {
			e.buf.cursor.Col = col
			e.buf.wantCol = col
		}
	}
}

// ---- normal mode ----

func (e *Editor) keyNormal(key string) {
	e.message = ""
	// Check for pending find character (f/F/t/T) first, before count and shift (F-133)
	if e.handlePendFind(key) {
		return
	}
	if e.pendObj != 0 {
		e.finishObject(key)
		return
	}
	if e.pendShift != "" && key != e.pendShift {
		e.pendShift = "" // >x: the shift is abandoned, x runs on its own
	}
	if text, ok := strings.CutPrefix(key, "paste:"); ok {
		e.clearPending()
		e.buf.InsertString(text) // a terminal paste lands at the cursor, one undo step
		return
	}

	// count digits (0 only valid when a count is already started or as motion)
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		if key != "0" || len(e.cntDigits) > 0 {
			e.cntDigits = append(e.cntDigits, rune(key[0]))
			return
		}
	}
	count := 1
	if len(e.cntDigits) > 0 {
		count, _ = strconv.Atoi(string(e.cntDigits))
	}

	motion, isMotion := normalMotion(key)

	if e.pendingOp != opNone {
		switch {
		case e.pendingOp == opComment && key == "c": // gcc
			e.beginOp(opComment)
			return
		case isMotion:
			count *= max(e.pendingCnt, 1) // 2d3w is 6 words, 2dj is 3 lines
			a, z := e.buf.spanFor(motion, count)
			linewise := motion.linewise()
			if e.pendingOp == opChange && !linewise {
				// vim special case: cw/cb behave like ce (keep trailing ws)
				if motion == motWordFwd || motion == motWordBack {
					a2, z2 := e.buf.spanFor(motWordEnd, count)
					a, z = a2, z2
				}
			}
			if lo, hi, ok := e.buf.ClosedFoldAt(e.buf.Cursor().Line); ok && !linewise {
				a, z, linewise = Pos{Line: lo}, Pos{Line: hi + 1}, true // dl on a closed fold takes all of it (F-176)
			}
			e.runOperator(e.pendingOp, a, z, linewise)
			e.clearPending()
			return
		case key == "zj" || key == "zk": // dzj: down to the next fold, linewise (F-176)
			e.foldOperator(key, count*max(e.pendingCnt, 1))
			return
		case key == ";" || key == ",":
			// d; d, : the last find as an operator motion (F-133)
			kind := e.lastFind.kind
			if key == "," {
				kind = reverseFind(kind)
			}
			if col := e.findCharCol(kind, e.lastFind.ch, count); kind != 0 && col >= 0 {
				a, z := e.buf.spanForFind(kind, col)
				e.runOperator(e.pendingOp, a, z, false)
			}
			e.clearPending()
			return
		case key == "%":
			// d% : inclusive of both brackets (F-133)
			if col := e.buf.matchBracket(); col >= 0 {
				cur := e.buf.cursor
				lo, hi := min(cur.Col, col), max(cur.Col, col)
				// from a non-bracket, % first moves to the bracket ahead; span from the cursor
				e.runOperator(e.pendingOp, Pos{Line: cur.Line, Col: lo}, Pos{Line: cur.Line, Col: hi + 1}, false)
			}
			e.clearPending()
			return
		case key == "i" || key == "a": // d/c/y/gc + ip, aw, i( ... (F-167)
			e.pendObj = []rune(key)[0]
			return
		case key == "esc":
			e.clearPending()
			return
		}
	}

	// Handle find motions (f/F/t/T) which need to wait for a character
	if key == "f" || key == "F" || key == "t" || key == "T" {
		e.pendFind = []rune(key)[0]
		return
	}

	if !isMotion {
		if e.normalAction(key, count) {
			return
		}
		// unknown: fall through to operator start below for d/c/y handled here
	}

	switch key {
	case "gc": // comment operator (F-150); the surface sends the g-chord whole
		if e.pendingOp != opNone && e.pendingOp != opComment {
			e.clearPending() // dgc: no such motion
			return
		}
		e.beginOp(opComment)
	case "]x", "[x": // next / previous merge conflict (F-156); the surface sends the chord whole
		if e.pendingOp != opNone {
			e.clearPending() // d]x: no such motion
			return
		}
		dir := 1
		if key == "[x" {
			dir = -1
		}
		e.jumpConflict(dir, count)
		e.clearPending()
	case "ctrl+/", "ctrl+_": // toggle this line (count lines); many terminals send ctrl+_
		e.toggleCommentHere(count)
		e.clearPending()
	case "d":
		e.beginOp(opDelete)
	case "c":
		e.beginOp(opChange)
	case "y":
		e.beginOp(opYank)
	case "esc":
		e.hlOff = true // like :noh
	default:
		if isMotion && motion == motEOF && len(e.cntDigits) > 0 { // 42G (F-119)
			e.GotoLine(count)
			e.cntDigits = nil
		} else if isMotion {
			e.buf.move(motion, count)
		} else if len(e.cntDigits) > 0 {
			e.message = ""
			e.cntDigits = nil
		}
	}
}

// runOperator applies delete/change/yank over a half-open range.
func (e *Editor) runOperator(o op, a, z Pos, linewise bool) {
	if o == opComment {
		e.toggleComment(commentLines(a, z))
		return
	}
	text := e.buf.yankRange(a, z)
	switch o {
	case opDelete:
		e.register, e.registerLW = text, linewise
		e.buf.deleteRange(a, z)
		e.message = opMessage(o, linewise)
	case opYank:
		e.register, e.registerLW = text, linewise
		e.yanked = &text
		e.buf.SetCursor(a)
		e.message = opMessage(o, linewise)
	case opChange:
		e.register, e.registerLW = text, linewise
		inner := z.Line < e.buf.LineCount() // lines remain below: the blank to type on is new (a blank line there is the separator)
		e.buf.deleteRange(a, z)
		if linewise {
			if inner && a.Line < e.buf.LineCount() && e.buf.lines[a.Line] == "" {
				e.buf.lines = append(e.buf.lines[:a.Line+1], e.buf.lines[a.Line:]...)
			}
			e.ensureBlankLine(a)
		}
		e.mode = ModeInsert
	}
}

// ensureBlankLine guarantees an empty line exists at a.Line (used by
// linewise change so cc leaves editable space).
func (e *Editor) ensureBlankLine(at Pos) {
	line := at.Line
	if line >= len(e.buf.lines) {
		e.buf.lines = append(e.buf.lines, "")
		return
	}
	if e.buf.lines[line] != "" {
		newLines := append([]string{}, e.buf.lines[:line]...)
		newLines = append(newLines, "")
		newLines = append(newLines, e.buf.lines[line:]...)
		e.buf.lines = newLines
	}
	if e.buf.Cursor().Line != line {
		e.buf.cursor = Pos{Line: line, Col: 0}
	}
}

func (e *Editor) beginOp(o op) {
	if e.pendingOp == o { // doubled operator: linewise on current line ×count
		n := maxInt(maxInt(e.count(), e.pendingCnt), 1)
		start, z := e.buf.foldLines(e.buf.Cursor().Line, n) // a closed fold counts as one line (F-176)
		e.runOperator(o, Pos{start, 0}, Pos{z, 0}, true)
		e.clearPending()
		return
	}
	e.pendingOp = o
	e.pendingCnt = e.count()
	e.cntDigits = nil
}

func (e *Editor) clearPending() {
	e.pendingOp = opNone
	e.pendingCnt = 0
	e.cntDigits = nil
}

func (e *Editor) count() int {
	if len(e.cntDigits) == 0 {
		return 1
	}
	n, _ := strconv.Atoi(string(e.cntDigits))
	return n
}

func normalMotion(key string) (motion, bool) {
	switch key {
	case "h", "left":
		return motLeft, true
	case "l", "right", "space", " ":
		return motRight, true
	case "j", "down", "enter":
		return motDown, true
	case "k", "up":
		return motUp, true
	case "0", "home":
		return motLineStart, true
	case "$", "end":
		return motLineEnd, true
	case "w":
		return motWordFwd, true
	case "b":
		return motWordBack, true
	case "e":
		return motWordEnd, true
	case "G", "ctrl+end":
		return motEOF, true
	case "gg", "ctrl+home":
		return motBOF, true
	}
	return 0, false
}

// opMessage renders vim-style feedback; exact counts use newline/line
// math on the yanked text.
func opMessage(o op, linewise bool) string {
	verb := "changed"
	switch o {
	case opDelete:
		verb = "deleted"
	case opYank:
		verb = "yanked"
	}
	if !linewise {
		return verb + " text"
	}
	return verb + " lines"
}

func (e *Editor) normalAction(key string, count int) bool {
	if key == "x" || key == "delete" || key == "D" { // vim: a closed fold is deleted whole (F-176)
		if lo, hi, ok := e.buf.ClosedFoldAt(e.buf.Cursor().Line); ok {
			e.runOperator(opDelete, Pos{Line: lo}, Pos{Line: hi + 1}, true)
			return true
		}
	}
	switch key {
	case "i":
		e.mode = ModeInsert
	case "I":
		e.buf.move(motLineStart, 1)
		if t := e.buf.runes(e.buf.Cursor().Line); len(t) > 0 && isSpace(t[0]) {
			for i, r := range t {
				if !isSpace(r) {
					e.buf.cursor.Col = i
					break
				}
			}
		}
		e.mode = ModeInsert
	case "a":
		line := e.buf.runes(e.buf.Cursor().Line)
		if e.buf.Cursor().Col < len(line) {
			e.buf.cursor.Col++
		}
		e.mode = ModeInsert
	case "A":
		e.buf.cursor.Col = len(e.buf.runes(e.buf.Cursor().Line))
		e.mode = ModeInsert
	case "o": // open a line below, keeping the indent (one more after an opener)
		if _, hi, ok := e.buf.ClosedFoldAt(e.buf.Cursor().Line); ok {
			e.buf.cursor.Line = hi // below a closed fold, not inside it (F-176)
		}
		e.buf.cursor.Col = len(e.buf.runes(e.buf.Cursor().Line))
		e.newline()
		e.mode = ModeInsert
	case "O": // open a line above with this line's indent
		l := e.buf.Cursor().Line
		lead := leadingSpace(e.buf.lines[l])
		e.buf.insertAt(Pos{Line: l}, lead+"\n")
		e.buf.cursor = Pos{Line: l, Col: len([]rune(lead))}
		e.mode = ModeInsert
	case ";":
		// Repeat last find motion (F-133)
		if e.lastFind.kind != 0 {
			e.execFind(e.lastFind.kind, e.lastFind.ch, count)
		}
		return true
	case ",":
		// Reverse last find motion (F-133)
		if e.lastFind.kind != 0 {
			// Flip f↔F and t↔T
			e.execFind(reverseFind(e.lastFind.kind), e.lastFind.ch, count)
		}
		return true
	case "%":
		// Match bracket motion (F-133)
		col := e.buf.matchBracket()
		if col >= 0 {
			e.buf.cursor.Col = col
			e.buf.wantCol = col
		}
		return true
	case "x", "delete":
		for i := 0; i < count; i++ {
			if !e.buf.DeleteForward() {
				break
			}
		}
	case "X", "backspace":
		for i := 0; i < count; i++ {
			if !e.buf.DeleteBackspace() {
				break
			}
		}
	case "D":
		a := e.buf.Cursor()
		z := Pos{a.Line, len(e.buf.runes(a.Line))}
		e.register, e.registerLW = e.buf.yankRange(a, z), false
		e.buf.deleteRange(a, z)
	case "C":
		e.Key("D")
		e.mode = ModeInsert
	case "p":
		e.paste(count, true)
	case "P":
		e.paste(count, false)
	case "u":
		if !e.buf.Undo() {
			e.message = "already at oldest change"
		}
	case "ctrl+r":
		if !e.buf.Redo() {
			e.message = "already at newest change"
		}
	case "v", "V", "ctrl+v":
		e.mode = ModeVisual
		e.visualLine = key == "V"
		e.visualBlock, e.blockEOL = key == "ctrl+v", false
		e.visualStart = e.buf.Cursor()
	case ">", "<": // >> / << shift count lines (F-119)
		if e.pendShift == key {
			line := e.buf.Cursor().Line
			lo, end := e.buf.foldLines(line, count)
			e.shiftLines(lo, end-1, key == ">")
			e.pendShift = ""
			e.cntDigits = nil
			return true
		}
		e.pendShift = key
		return true
	case ":":
		e.mode = ModeCommand
		e.cmdline = nil
		e.cmdPrefix = ':'
	case "J":
		n := maxInt(count-1, 1) // 3J joins 3 lines
		for i := 0; i < n && e.buf.Cursor().Line < e.buf.LineCount()-1; i++ {
			next := e.buf.lines[e.buf.Cursor().Line+1]
			trimmed := strings.TrimLeft(next, " \t")
			sep := " "
			if trimmed == "" {
				sep = ""
			}
			e.buf.snapshotBefore()
			col := len(e.buf.runes(e.buf.Cursor().Line))
			e.buf.lines[e.buf.Cursor().Line] += sep + trimmed
			e.buf.lines = append(e.buf.lines[:e.buf.Cursor().Line+1], e.buf.lines[e.buf.Cursor().Line+2:]...)
			e.buf.cursor.Col = col
			e.buf.dirty = true
		}
	case "/":
		e.startSearch(1)
	case "?":
		e.startSearch(-1)
	case "n", "N":
		e.hlOff = false
		dir := e.lastDir
		if dir == 0 {
			dir = 1
		}
		if key == "N" {
			dir = -dir
		}
		e.searchNext(dir)
	case "*":
		if w := e.wordUnderCursor(); w != "" {
			e.runSearch(w, 1)
		}
	case "ctrl+d", "ctrl+u", "pgdown", "pgup":
		n := maxInt(e.page, 2)
		if key == "ctrl+d" || key == "ctrl+u" {
			n /= 2
		}
		if key == "ctrl+u" || key == "pgup" {
			n = -n
		}
		e.scroll(n)
	case "zz", "Z":
		// accept and ignore viewport niceties in MVP
	default:
		return false
	}
	e.clearPending()
	return true
}

func (e *Editor) paste(count int, after bool) {
	if e.register == "" {
		return
	}
	if e.blockRegValid() { // F-159: a block goes back in as a rectangle
		e.pasteBlock(count, after)
		return
	}
	if _, hi, ok := e.buf.ClosedFoldAt(e.buf.Cursor().Line); ok && after && e.registerLW {
		e.buf.cursor.Line = hi // linewise p lands below a closed fold (F-176)
	}
	for i := 0; i < maxInt(count, 1); i++ {
		end := e.buf.paste(e.register, e.registerLW, after)
		if e.registerLW {
			e.buf.cursor = end
		}
	}
}

// ---- visual mode ----

func (e *Editor) keyVisual(key string) {
	e.message = ""
	// Check for pending find character first (F-133)
	if e.pendFind != 0 {
		if e.handlePendFindVisual(key) {
			return
		}
	}
	if e.pendObj != 0 {
		e.visualObject(key)
		return
	}
	if strings.HasPrefix(key, "paste:") { // leave the selection, paste at the cursor
		e.mode = ModeNormal
		e.keyNormal(key)
		return
	}
	if e.visualBlock {
		if key == "esc" && e.pendRepl {
			e.pendRepl = false
			return
		}
		if blockHorizontal(key) {
			e.blockEOL = false
		}
		if key != "esc" && e.pendFind == 0 && e.keyBlock(key) {
			return
		}
	}
	if key == "esc" {
		e.mode = ModeNormal
		e.blockEOL = false
		return
	}
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		e.cntDigits = append(e.cntDigits, rune(key[0]))
		return
	}
	count := e.count()

	// Handle find motions in visual mode (F-133)
	if key == "f" || key == "F" || key == "t" || key == "T" {
		e.pendFind = []rune(key)[0]
		return
	}

	if motion, ok := normalMotion(key); ok {
		e.buf.move(motion, count)
		if e.visualBlock && motion == motLineEnd {
			e.blockEOL = true // $: to each line's end, kept by j/k
		}
		return
	}

	a, z := e.visualSpan()
	switch key {
	case ";":
		// Repeat last find motion in visual mode (F-133)
		if e.lastFind.kind != 0 {
			e.execFind(e.lastFind.kind, e.lastFind.ch, count)
		}
		return
	case ",":
		// Reverse last find motion in visual mode (F-133)
		if e.lastFind.kind != 0 {
			e.execFind(reverseFind(e.lastFind.kind), e.lastFind.ch, count)
		}
		return
	case "%":
		// Match bracket in visual mode (F-133)
		col := e.buf.matchBracket()
		if col >= 0 {
			e.buf.cursor.Col = col
			e.buf.wantCol = col
		}
		return
	case ":":
		// Like vim, ":" from visual mode opens the command line; the
		// selection is kept so ex commands (:ask) can still use it.
		if text, from, to, ok := e.Selection(); ok {
			e.lastSel = &Selected{Text: text, From: from, To: to}
		}
		lo, hi := order(e.visualStart, e.buf.Cursor())
		e.markLo, e.markHi, e.markSet = lo.Line, hi.Line, true // '< and '> (F-137)
		e.mode = ModeCommand
		e.cmdline = nil
		e.cmdPrefix = ':'
	case ">", "<":
		lo, hi := order(e.visualStart, e.buf.Cursor())
		e.shiftLines(lo.Line, hi.Line, key == ">")
		e.mode = ModeNormal
	case "gc", "ctrl+/", "ctrl+_": // comment the selected lines (F-150)
		lo, hi := order(e.visualStart, e.buf.Cursor())
		e.toggleComment(lo.Line, hi.Line)
		e.mode = ModeNormal
	case "d", "x":
		e.register, e.registerLW = e.buf.yankRange(a, z), e.visualLine
		if e.visualLine && !strings.HasSuffix(e.register, "\n") {
			e.register = strings.TrimPrefix(e.register, "\n") + "\n"
		}
		e.buf.deleteRange(a, z)
		e.mode = ModeNormal
	case "y":
		e.register, e.registerLW = e.buf.yankRange(a, z), e.visualLine
		e.yanked = &e.register
		e.buf.SetCursor(a)
		e.mode = ModeNormal
	case "c":
		e.register, e.registerLW = e.buf.yankRange(a, z), false
		e.buf.deleteRange(a, z)
		e.mode = ModeInsert
	case "v", "V", "ctrl+v": // switch the kind of selection; the same key leaves visual mode
		switch {
		case key == "V" && !e.visualLine, key == "ctrl+v":
			e.visualLine, e.visualBlock = key == "V", key == "ctrl+v"
		case key == "v" && e.visualLine:
			e.visualLine = false
		default:
			e.mode = ModeNormal
		}
	case "o":
		s := e.visualStart
		e.visualStart = e.buf.Cursor()
		e.buf.SetCursor(s)
	case "i", "a": // iw, a(, ip ... select the object (F-167)
		if !e.visualBlock {
			e.pendObj = []rune(key)[0]
		}
	default:
		// ignore unknowns
	}
}

// blockHorizontal reports the keys that end a "$" block's reach to each
// line's end (j and k keep it).
func blockHorizontal(key string) bool {
	switch key {
	case "h", "l", "left", "right", "space", " ", "0", "home", "w", "b", "e", "f", "F", "t", "T", ";", ",", "%":
		return true
	}
	return false
}

// handlePendFindVisual processes the character input for f/F/t/T motions in visual mode.
func (e *Editor) handlePendFindVisual(key string) bool {
	if e.pendFind == 0 {
		return false
	}
	var ch rune
	if len(key) == 1 {
		ch = []rune(key)[0]
	} else if key == "space" {
		ch = ' '
	} else {
		// Multi-char or special key: cancel the find
		e.pendFind = 0
		return false
	}

	e.lastFind.kind = e.pendFind
	e.lastFind.ch = ch
	e.execFind(e.pendFind, ch, e.count())
	e.pendFind = 0
	return true
}

// visualSpan orders the anchor and cursor into a half-open range.
func (e *Editor) visualSpan() (Pos, Pos) {
	cur := e.buf.Cursor()
	a, z := order(e.visualStart, cur)
	if e.visualLine { // whole lines: from the first line's start to the next line's start
		a.Col = 0
		a.Line, z.Line = e.buf.foldWiden(a.Line, z.Line) // a closed fold at either end goes whole (F-176)
		if z.Line+1 < e.buf.LineCount() {
			return a, Pos{Line: z.Line + 1}
		}
		if a.Line > 0 { // the last line: take the newline before it instead
			return Pos{Line: a.Line - 1, Col: len(e.buf.runes(a.Line - 1))}, Pos{Line: z.Line, Col: len(e.buf.runes(z.Line))}
		}
		return a, Pos{Line: z.Line, Col: len(e.buf.runes(z.Line))}
	}
	// include the character under the cursor like real visual mode
	max := len(e.buf.runes(z.Line))
	if z.Col < max {
		z.Col++
	}
	return a, z
}

// ---- command mode ----

func (e *Editor) keyCommand(key string) {
	switch key {
	case "esc":
		e.mode = ModeNormal
		e.cmdline = nil
	case "enter":
		line := string(e.cmdline)
		e.cmdline = nil
		switch e.cmdPrefix {
		case '/':
			e.runSearch(line, 1)
		case '?':
			e.runSearch(line, -1)
		default:
			e.execCommand(line)
		}
		e.cmdPrefix = ':'
	case "backspace":
		if len(e.cmdline) > 0 {
			e.cmdline = e.cmdline[:len(e.cmdline)-1]
		} else {
			e.mode = ModeNormal
		}
	case "ctrl+u":
		e.cmdline = nil
	case "ctrl+w", "alt+backspace":
		e.cmdline = []rune(dropWordBack(string(e.cmdline)))
	default:
		if text, ok := strings.CutPrefix(key, "paste:"); ok {
			e.cmdline = append(e.cmdline, []rune(strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", " "))...)
			return
		}
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			e.cmdline = append(e.cmdline, r...)
		}
	}
}

func (e *Editor) execCommand(cmd string) {
	e.mode = ModeNormal
	cmd = strings.TrimSpace(cmd)
	switch cmd {
	case "":
		return
	case "w", "w!":
		save := e.Save
		if cmd == "w!" {
			save = e.SaveForce
		}
		if err := save(); err != nil {
			e.message = err.Error()
		} else {
			e.message = `"` + e.path + `" written` + e.saveNote
		}
	case "q":
		if e.buf.Dirty() && !e.closeForced {
			e.message = "no write since last change (:q! overrides)"
			return
		}
		e.closeReq, e.closeForced = true, false
	case "q!":
		e.closeReq, e.closeForced = true, true
	case "cq", "cq!": // quit with failure (F-165)
		e.aborted = true
		e.closeReq, e.closeForced = true, true
	case "wq", "x":
		if err := e.Save(); err != nil {
			e.message = err.Error()
			return
		}
		e.closeReq, e.closeForced = true, false
	case "e", "e!":
		if e.path == "" {
			e.message = "no file name"
			return
		}
		if cmd == "e" && e.buf.Dirty() {
			e.message = "unsaved changes (:e! discards them)"
			return
		}
		if err := e.Reload(); err != nil {
			e.message = err.Error()
		}
	default:
		if cmd == "noh" || cmd == "nohlsearch" {
			e.hlOff = true
			return
		}
		if arg, ok := strings.CutPrefix(cmd, "cursor "); ok { // F-174
			e.mcCommand(strings.TrimSpace(arg))
			return
		}
		if arg, ok := strings.CutPrefix(cmd, "conflict"); ok && (arg == "" || arg[0] == ' ') { // F-156
			e.conflictCommand(strings.TrimSpace(arg))
			return
		}
		if n, err := strconv.Atoi(cmd); err == nil { // :42 (F-119)
			e.GotoLine(n)
			return
		}
		if exSubstitute(e, cmd) {
			return
		}
		if e.delegate != nil && e.delegate.ExecEx(e, cmd) {
			return
		}
		e.message = "not an editor command: " + cmd
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// GotoLine puts the cursor on the first non-blank of 1-based line n,
// clamped to the buffer.
func (e *Editor) GotoLine(n int) {
	line := min(max(n-1, 0), e.buf.LineCount()-1)
	e.buf.SetCursor(Pos{Line: line, Col: firstNonSpaceRune(e.buf.Line(line))})
}

// Save writes to the bound path; it refuses (ErrChangedOnDisk) when the
// file changed since the buffer read it.
func (e *Editor) Save() error { return e.save(false) }

// SaveForce writes to the bound path even when the file changed on disk.
func (e *Editor) SaveForce() error { return e.save(true) }

func (e *Editor) save(force bool) error {
	if e.path == "" {
		e.message = "no file name"
		return errNoFile
	}
	if !force { // check before format-on-save edits the buffer
		if clash, err := e.buf.conflict(e.path); err != nil {
			return err
		} else if clash {
			return ErrChangedOnDisk
		}
	}
	e.saveNote = ""
	if e.beforeSave != nil {
		e.saveNote = e.beforeSave(e)
	}
	if err := e.buf.SaveForce(e.path); err != nil {
		return err
	}
	if e.afterSave != nil {
		e.afterSave(e)
	}
	return nil
}

// Reload replaces the buffer with the file's current content, keeping the
// cursor where it was (clamped) and reporting it through TakeReloaded.
func (e *Editor) Reload() error {
	b, err := Open(e.path)
	if err != nil {
		return err
	}
	cur := e.buf.Cursor()
	b.seq = e.buf.seq + 1 // render caches keyed on seq must not reuse the old text
	b.SetCursor(cur)
	b.fold = e.buf.fold // the folds are mapped across the new text on first use (F-176)
	e.buf = b
	e.reloaded = true
	e.mode = ModeNormal
	return nil
}

// SetAfterSave registers a hook that runs after :w / :wq wrote the file.
func (e *Editor) SetAfterSave(fn func(*Editor)) { e.afterSave = fn }

// SetBeforeSave registers a hook that runs just before :w / :wq write the
// file (format-on-save). The hook may edit the buffer; a non-empty return
// is shown after the "written" message, and a failing hook never blocks
// the write.
func (e *Editor) SetBeforeSave(fn func(*Editor) string) { e.beforeSave = fn }

type simpleError string

func (s simpleError) Error() string { return string(s) }

const errNoFile = simpleError("no file name")
