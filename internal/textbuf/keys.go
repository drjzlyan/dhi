package textbuf

import (
	"strconv"
	"strings"
)

// Key feeds one keystroke (bubbletea msg.String() style: "a", "enter",
// "esc", "backspace", "up", "down", …). It is the entire modal surface.
func (e *Editor) Key(key string) {
	was := e.mode
	e.KeyUngrouped(key)
	// Insert sessions undo as one step: open the group on entering
	// insert mode, close it on leaving.
	if e.mode == ModeInsert && was != ModeInsert {
		e.buf.BeginUndoGroup()
	}
	if was == ModeInsert && e.mode != ModeInsert {
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

// ---- normal mode ----

func (e *Editor) keyNormal(key string) {
	e.message = ""

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
		case isMotion:
			a, z := e.buf.spanFor(motion, count)
			linewise := motion.linewise()
			if e.pendingOp == opChange && !linewise {
				// vim special case: cw/cb behave like ce (keep trailing ws)
				if motion == motWordFwd || motion == motWordBack {
					a2, z2 := e.buf.spanFor(motWordEnd, count)
					a, z = a2, z2
				}
			}
			e.runOperator(e.pendingOp, a, z, linewise)
			e.clearPending()
			return
		case key == "esc":
			e.clearPending()
			return
		}
	}

	if !isMotion {
		if e.normalAction(key, count) {
			return
		}
		// unknown: fall through to operator start below for d/c/y handled here
	}

	switch key {
	case "d":
		e.beginOp(opDelete)
	case "c":
		e.beginOp(opChange)
	case "y":
		e.beginOp(opYank)
	case "esc":
		// nothing pending; ignore
	default:
		if isMotion {
			e.buf.move(motion, count)
		} else if len(e.cntDigits) > 0 {
			e.message = ""
			e.cntDigits = nil
		}
	}
}

// runOperator applies delete/change/yank over a half-open range.
func (e *Editor) runOperator(o op, a, z Pos, linewise bool) {
	text := e.buf.yankRange(a, z)
	switch o {
	case opDelete:
		e.register, e.registerLW = text, linewise
		e.buf.deleteRange(a, z)
		e.message = opMessage(o, linewise)
	case opYank:
		e.register, e.registerLW = text, linewise
		e.buf.SetCursor(a)
		e.message = opMessage(o, linewise)
	case opChange:
		e.register, e.registerLW = text, linewise
		e.buf.deleteRange(a, z)
		if linewise {
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
		start := e.buf.Cursor().Line
		z := minInt(start+n, e.buf.LineCount())
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
	case "l", "right", "space":
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
	case "G":
		return motEOF, true
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
	case "o":
		e.buf.cursor.Col = len(e.buf.runes(e.buf.Cursor().Line))
		e.buf.BreakLine()
		e.mode = ModeInsert
	case "O":
		if e.buf.Cursor().Line == 0 {
			e.buf.cursor = Pos{}
			e.buf.insertAt(Pos{}, "\n")
			e.buf.cursor = Pos{Line: 0, Col: 0}
		} else {
			e.buf.cursor.Line--
			e.buf.cursor.Col = len(e.buf.runes(e.buf.Cursor().Line))
			e.buf.BreakLine()
		}
		e.mode = ModeInsert
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
	case "v":
		e.mode = ModeVisual
		e.visualStart = e.buf.Cursor()
	case ":":
		e.mode = ModeCommand
		e.cmdline = nil
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
	case "n", "N", "/", "?", "*":
		e.message = "search arrives with a later chunk"
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
	if key == "esc" {
		e.mode = ModeNormal
		return
	}
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		e.cntDigits = append(e.cntDigits, rune(key[0]))
		return
	}
	count := e.count()

	if motion, ok := normalMotion(key); ok {
		e.buf.move(motion, count)
		return
	}

	a, z := e.visualSpan()
	switch key {
	case "d", "x":
		e.register, e.registerLW = e.buf.yankRange(a, z), false
		e.buf.deleteRange(a, z)
		e.mode = ModeNormal
	case "y":
		e.register, e.registerLW = e.buf.yankRange(a, z), false
		e.buf.SetCursor(a)
		e.mode = ModeNormal
	case "c":
		e.register, e.registerLW = e.buf.yankRange(a, z), false
		e.buf.deleteRange(a, z)
		e.mode = ModeInsert
	case "o":
		s := e.visualStart
		e.visualStart = e.buf.Cursor()
		e.buf.SetCursor(s)
	default:
		// ignore unknowns
	}
}

// visualSpan orders the anchor and cursor into a half-open range.
func (e *Editor) visualSpan() (Pos, Pos) {
	cur := e.buf.Cursor()
	a, z := order(e.visualStart, cur)
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
		e.execCommand(string(e.cmdline))
		e.cmdline = nil
	case "backspace":
		if len(e.cmdline) > 0 {
			e.cmdline = e.cmdline[:len(e.cmdline)-1]
		} else {
			e.mode = ModeNormal
		}
	default:
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
	case "w":
		if err := e.Save(); err != nil {
			e.message = err.Error()
		} else {
			e.message = `"` + e.path + `" written`
		}
	case "q":
		if e.buf.Dirty() && !e.closeForced {
			e.message = "no write since last change (:q! overrides)"
			return
		}
		e.closeReq, e.closeForced = true, false
	case "q!":
		e.closeReq, e.closeForced = true, true
	case "wq", "x":
		if err := e.Save(); err != nil {
			e.message = err.Error()
			return
		}
		e.closeReq, e.closeForced = true, false
	case "e":
		if e.path == "" {
			e.message = "no file name"
			return
		}
		b, err := Open(e.path)
		if err != nil {
			e.message = err.Error()
			return
		}
		e.buf = b
		e.reloaded = true
	default:
		if e.delegate != nil && e.delegate.ExecEx(e, cmd) {
			return
		}
		e.message = "not an editor command: " + cmd
	}
}

// Save writes to the bound path.
func (e *Editor) Save() error {
	if e.path == "" {
		e.message = "no file name"
		return errNoFile
	}
	return e.buf.Save(e.path)
}

type simpleError string

func (s simpleError) Error() string { return string(s) }

const errNoFile = simpleError("no file name")
