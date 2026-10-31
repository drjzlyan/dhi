package textbuf

import (
	"strings"
	"unicode"
)

// Smart insert (F-119): Enter keeps the line's indent (one more after an
// opening bracket or a trailing colon), Tab inserts the file's indent
// unit, brackets and quotes pair, and Backspace undoes a pair or a soft
// tab. The surface sets the unit from the language (tabs for Go, two
// spaces for TypeScript…) or from the file itself.

// SetIndent sets what Tab inserts and Enter adds after an opener: "\t"
// or a run of spaces. Empty keeps the current unit.
func (e *Editor) SetIndent(unit string) {
	if unit != "" {
		e.indent = unit
	}
}

// Indent is the current indent unit.
func (e *Editor) Indent() string {
	if e.indent == "" {
		return "\t"
	}
	return e.indent
}

// SetAutoPair turns bracket and quote pairing on or off.
func (e *Editor) SetAutoPair(on bool) { e.noPair = !on }

var pairs = map[rune]rune{'(': ')', '[': ']', '{': '}', '"': '"', '\'': '\'', '`': '`'}

func isCloser(r rune) bool { return r == ')' || r == ']' || r == '}' }

func isQuote(r rune) bool { return r == '"' || r == '\'' || r == '`' }

// around returns the runes just before and at the cursor (0 when none).
func (e *Editor) around() (prev, next rune) {
	c := e.buf.Cursor()
	line := e.buf.runes(c.Line)
	if c.Col > 0 && c.Col <= len(line) {
		prev = line[c.Col-1]
	}
	if c.Col < len(line) {
		next = line[c.Col]
	}
	return prev, next
}

// typeRune inserts r with pairing; false means "insert it plainly".
func (e *Editor) typeRune(r rune) bool {
	if e.noPair {
		return false
	}
	prev, next := e.around()
	switch {
	case (isCloser(r) || isQuote(r)) && next == r: // type over the closer
		e.buf.move(motRight, 1)
		return true
	case r == '(' || r == '[' || r == '{':
		if next != 0 && !unicode.IsSpace(next) && !isCloser(next) && next != ',' && next != ';' {
			return false
		}
	case isQuote(r):
		if isWordRune(prev) || prev == r || (next != 0 && !unicode.IsSpace(next) && !isCloser(next) && next != ',' && next != ';') {
			return false // don't, it's, an apostrophe or a closing quote
		}
	default:
		return false
	}
	e.buf.InsertString(string(r) + string(pairs[r]))
	e.buf.move(motLeft, 1)
	return true
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// newline breaks the line keeping its indent; after an opener it indents
// one more, and between a pair ({|}) it opens a block.
func (e *Editor) newline() {
	c := e.buf.Cursor()
	line := e.buf.runes(c.Line)
	lead := leadingSpace(string(line))
	before := strings.TrimRightFunc(string(line[:min(c.Col, len(line))]), unicode.IsSpace)
	prev, next := e.around()
	deeper := strings.HasSuffix(before, "{") || strings.HasSuffix(before, "(") ||
		strings.HasSuffix(before, "[") || strings.HasSuffix(before, ":")
	if deeper && pairs[prev] == next && isCloser(next) { // {|} → block
		e.buf.InsertString("\n" + lead + e.Indent() + "\n" + lead)
		e.buf.move(motUp, 1)
		e.buf.cursor.Col = len([]rune(lead + e.Indent()))
		return
	}
	if deeper {
		lead += e.Indent()
	}
	e.buf.InsertString("\n" + lead)
}

// backspace removes an empty pair or a whole soft tab before the cursor.
func (e *Editor) backspace() {
	prev, next := e.around()
	if !e.noPair && prev != 0 && pairs[prev] == next && next != 0 {
		e.buf.DeleteForward()
		e.buf.DeleteBackspace()
		return
	}
	if unit := e.Indent(); unit != "\t" {
		c := e.buf.Cursor()
		before := string(e.buf.runes(c.Line)[:c.Col])
		if c.Col > 0 && strings.TrimLeft(before, " ") == "" {
			n := c.Col % len(unit)
			if n == 0 {
				n = len(unit)
			}
			for i := 0; i < n; i++ {
				e.buf.DeleteBackspace()
			}
			return
		}
	}
	e.buf.DeleteBackspace()
}

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// DetectIndent guesses a file's indent unit from its lines: "\t" when
// tab-indented lines win, else the smallest space step seen (2, 4 or 8);
// "" when the text says nothing.
func DetectIndent(text string) string {
	tabs, spaces := 0, 0
	step := 0
	for _, l := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(l, "\t"):
			tabs++
		case strings.HasPrefix(l, " ") && strings.TrimSpace(l) != "":
			spaces++
			n := len(l) - len(strings.TrimLeft(l, " "))
			if n >= 2 && (step == 0 || n < step) {
				step = n
			}
		}
	}
	switch {
	case tabs == 0 && spaces == 0:
		return ""
	case tabs >= spaces:
		return "\t"
	case step == 0:
		return ""
	}
	return strings.Repeat(" ", min(step, 8))
}

// shiftLines indents (in) or outdents lines lo..hi by one unit, as one
// undo step; blank lines are left alone.
func (e *Editor) shiftLines(lo, hi int, in bool) {
	unit := e.Indent()
	e.buf.snapshotBefore()
	for l := lo; l <= hi && l < len(e.buf.lines); l++ {
		line := e.buf.lines[l]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if in {
			e.buf.lines[l] = unit + line
			continue
		}
		switch {
		case strings.HasPrefix(line, "\t"):
			e.buf.lines[l] = line[1:]
		default:
			n := len(line) - len(strings.TrimLeft(line, " "))
			e.buf.lines[l] = line[min(n, max(len(unit), 1)):]
		}
	}
	e.buf.dirty = true
	e.buf.seq++
	e.buf.SetCursor(Pos{Line: lo, Col: firstNonSpaceRune(e.buf.lines[lo])})
}

// record collects the keys of a change for "." (F-119): a normal-mode
// command and, when it enters insert mode or waits for a motion, every
// key until normal mode is back.
func (e *Editor) record(key string) {
	if e.replaying {
		return
	}
	if e.rec == nil {
		if e.mode != ModeNormal {
			return
		}
		e.recSeq = e.buf.seq
	}
	e.rec = append(e.rec, key)
}

func (e *Editor) recordDone() {
	if e.replaying || e.rec == nil {
		return
	}
	if e.mode != ModeNormal || e.pendingOp != opNone || e.pendShift != "" || len(e.cntDigits) > 0 {
		return // the change is still being made
	}
	if e.buf.seq != e.recSeq && e.rec[0] != "u" && e.rec[0] != "ctrl+r" {
		e.lastChange = e.rec
	}
	e.rec = nil
}

// repeat replays the last change at the cursor.
func (e *Editor) repeat() {
	if len(e.lastChange) == 0 {
		return
	}
	e.replaying = true
	for _, k := range e.lastChange {
		e.Key(k)
	}
	e.replaying = false
}
