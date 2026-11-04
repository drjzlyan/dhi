package textbuf

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Multi-cursor (F-174). The primary cursor stays the buffer's own; the
// secondary ones live in Editor.mc. While any exist, a key runs once at
// every cursor, last position first, so an edit never shifts a cursor that
// has yet to run; the other cursors are carried across each edit by diffing
// the text before and after (no hook in the editing primitives, so every
// command works at every cursor). The whole key is one undo step, and
// cursors that land on the same spot merge.

// mcur is one cursor while a key runs.
type mcur struct {
	pos     Pos
	want    int // sticky column for j/k
	primary bool
	dead    bool // swallowed by another cursor's edit

	reg    string // this cursor's own yank / delete register, so p pastes what it took
	regLW  bool
	hasReg bool
}

type multi struct {
	extra []mcur // secondary cursors, sorted, never on the primary

	// The occurrence being extended (gb): cursors sit off runes into
	// each match of needle; whole matches whole words only.
	needle  string
	whole   bool
	off     int
	last    Pos          // where the newest cursor went; the next search starts after it
	skipped map[Pos]bool // occurrences skipped (gB), not offered again
}

// Cursors returns the secondary cursors in document order (rendering).
func (e *Editor) Cursors() []Pos {
	if e.mc == nil {
		return nil
	}
	out := make([]Pos, len(e.mc.extra))
	for i, c := range e.mc.extra {
		out[i] = c.pos
	}
	return out
}

// CursorCount is the number of cursors, the primary included.
func (e *Editor) CursorCount() int {
	if e.mc == nil {
		return 1
	}
	return 1 + len(e.mc.extra)
}

// CollapseCursors drops every secondary cursor.
func (e *Editor) CollapseCursors() { e.mc = nil }

// mcIdle reports no half-typed command (operator, count, find, object).
func (e *Editor) mcIdle() bool {
	return e.pendingOp == opNone && len(e.cntDigits) == 0 && e.pendFind == 0 &&
		e.pendObj == 0 && e.pendShift == "" && !e.pendRepl
}

// isMCKey is the cheap pre-check that keeps the single-cursor path free.
func isMCKey(key string) bool {
	switch key {
	case "gb", "gB", "alt+n", "alt+up", "alt+down":
		return true
	}
	return false
}

// mcKey handles the cursor-management keys; it reports whether key was one.
// gb / alt+n add the next occurrence, gB skips it, alt+up / alt+down add a
// cursor above / below; esc collapses (only when no command is half typed).
func (e *Editor) mcKey(key string) bool {
	if e.inMulti || !e.mcIdle() {
		return false
	}
	switch key {
	case "gb", "alt+n":
		if e.mode == ModeNormal || e.mode == ModeVisual {
			e.AddCursorNext()
			return true
		}
	case "gB":
		if e.mode == ModeNormal {
			e.SkipCursor()
			return true
		}
	case "alt+up", "alt+down":
		if e.mode == ModeNormal || e.mode == ModeInsert {
			e.AddCursorVertical(map[string]int{"alt+up": -1, "alt+down": 1}[key])
			return true
		}
	case "esc":
		if e.mc != nil && e.mode == ModeNormal {
			e.mc = nil
			e.message = ""
			return true
		}
	}
	return false
}

// occurrence is one match of the needle: its start.
func (e *Editor) occurrences(needle string, whole bool) []Pos {
	nr := []rune(needle)
	var out []Pos
	for l, line := range e.buf.lines {
		if !strings.Contains(line, needle) {
			continue
		}
		rs := []rune(line)
		for c := 0; c+len(nr) <= len(rs); c++ {
			if !runesAt(rs, c, nr) {
				continue
			}
			if whole && ((c > 0 && isWordRune(rs[c-1])) || (c+len(nr) < len(rs) && isWordRune(rs[c+len(nr)]))) {
				continue
			}
			out = append(out, Pos{Line: l, Col: c})
		}
	}
	return out
}

func runesAt(rs []rune, at int, sub []rune) bool {
	for i, r := range sub {
		if rs[at+i] != r {
			return false
		}
	}
	return true
}

// wordBounds is the word under col on a line, as rune offsets.
func wordBounds(rs []rune, col int) (int, int, bool) {
	if col >= len(rs) || !isWordRune(rs[col]) {
		return 0, 0, false
	}
	a, z := col, col
	for a > 0 && isWordRune(rs[a-1]) {
		a--
	}
	for z < len(rs) && isWordRune(rs[z]) {
		z++
	}
	return a, z, true
}

// AddCursorNext adds a cursor at the next occurrence of the word under the
// cursor (or of the visual selection), placed at the same offset in the
// match, wrapping around the buffer. The first press picks the needle; later
// presses extend it even if the cursor has moved.
func (e *Editor) AddCursorNext() bool {
	if e.mc == nil || e.mc.needle == "" {
		if !e.mcPickNeedle() {
			return false
		}
	}
	mc := e.mc
	occ := e.occurrences(mc.needle, mc.whole)
	taken := map[Pos]bool{e.buf.cursor: true}
	for _, c := range mc.extra {
		taken[c.pos] = true
	}
	// Offer the first free occurrence after the newest cursor, wrapping.
	var pick *Pos
	for pass := 0; pass < 2 && pick == nil; pass++ {
		for _, o := range occ {
			p := e.buf.clampPos(Pos{Line: o.Line, Col: o.Col + mc.off})
			if taken[p] || mc.skipped[p] {
				continue
			}
			if (pass == 0) == mc.last.before(p) {
				q := p
				pick = &q
				break
			}
		}
	}
	if pick == nil {
		e.message = "no more occurrences"
		e.mcNormalize()
		return false
	}
	mc.last = *pick
	mc.extra = append(mc.extra, mcur{pos: *pick, want: pick.Col})
	e.mcNormalize()
	e.message = itoa(e.CursorCount()) + " cursors"
	return true
}

// mcPickNeedle fixes what gb extends from: a single-line visual selection
// (plain text) or the word under the cursor (whole words).
func (e *Editor) mcPickNeedle() bool {
	var needle string
	whole, off := true, 0
	if e.mode == ModeVisual && !e.visualBlock && !e.visualLine {
		text, from, _, ok := e.Selection()
		if !ok || text == "" || strings.Contains(text, "\n") {
			e.message = "select text on one line (or put the cursor on a word)"
			return false
		}
		needle, whole = text, false
		e.mode = ModeNormal
		e.blockEOL = false
		e.buf.cursor, e.buf.wantCol = from, from.Col // the cursor sits at the selection's start
	} else {
		c := e.buf.cursor
		rs := e.buf.runes(c.Line)
		a, z, ok := wordBounds(rs, c.Col)
		if !ok {
			e.message = "no word under the cursor"
			return false
		}
		needle, off = string(rs[a:z]), c.Col-a
	}
	if e.mc == nil {
		e.mc = &multi{}
	}
	e.mc.needle, e.mc.whole, e.mc.off = needle, whole, off
	e.mc.last = e.buf.cursor
	return true
}

// SkipCursor drops the newest cursor from gb and moves on to the next
// occurrence (VS Code's "skip this one").
func (e *Editor) SkipCursor() bool {
	mc := e.mc
	if mc == nil || mc.needle == "" {
		e.message = "nothing to skip (gb adds an occurrence first)"
		return false
	}
	for i, c := range mc.extra {
		if c.pos == mc.last {
			mc.extra = append(mc.extra[:i], mc.extra[i+1:]...)
			if mc.skipped == nil {
				mc.skipped = map[Pos]bool{}
			}
			mc.skipped[c.pos] = true
			if !e.AddCursorNext() && len(mc.extra) == 0 {
				e.mc = nil
			}
			return true
		}
	}
	e.message = "nothing to skip"
	return false
}

// AddCursorVertical adds a cursor one line above the topmost (dir < 0) or
// below the bottommost (dir > 0) cursor, at the primary's sticky column.
func (e *Editor) AddCursorVertical(dir int) bool {
	line := e.buf.cursor.Line
	if e.mc != nil {
		for _, c := range e.mc.extra {
			if dir < 0 {
				line = min(line, c.pos.Line)
			} else {
				line = max(line, c.pos.Line)
			}
		}
	}
	line += dir
	if line < 0 || line >= len(e.buf.lines) {
		e.message = "no line " + map[bool]string{true: "above", false: "below"}[dir < 0]
		return false
	}
	want := e.buf.wantCol
	col := min(want, len(e.buf.runes(line)))
	if e.mode == ModeNormal && col > 0 && col >= len(e.buf.runes(line)) {
		col = len(e.buf.runes(line)) - 1
	}
	if e.mc == nil {
		e.mc = &multi{}
	}
	e.mc.extra = append(e.mc.extra, mcur{pos: Pos{Line: line, Col: col}, want: want})
	e.mcNormalize()
	e.message = itoa(e.CursorCount()) + " cursors"
	return true
}

// mcNormalize sorts the secondary cursors and merges any that coincide
// with each other or the primary.
func (e *Editor) mcNormalize() {
	mc := e.mc
	if mc == nil {
		return
	}
	sort.SliceStable(mc.extra, func(i, j int) bool { return mc.extra[i].pos.before(mc.extra[j].pos) })
	out := mc.extra[:0]
	for _, c := range mc.extra {
		if c.pos == e.buf.cursor || (len(out) > 0 && out[len(out)-1].pos == c.pos) {
			continue
		}
		out = append(out, c)
	}
	mc.extra = out
	if len(mc.extra) == 0 {
		e.mc = nil
	}
}

// mcCollapseKeys end multi-cursor mode before they run, once, at the
// primary: they are about a single place (search, ex line, visual, undo).
func mcCollapseKey(key string) bool {
	switch key {
	case ":", "/", "?", "n", "N", "*", "v", "V", "ctrl+v", "u", "ctrl+r",
		"ctrl+d", "ctrl+u", "pgdown", "pgup", "]x", "[x", "zz", "Z":
		return true
	}
	return false
}

// keyMulti runs key at every cursor as one undo step.
func (e *Editor) keyMulti(key string) {
	if e.mode == ModeNormal && e.mcIdle() && mcCollapseKey(key) {
		e.mc = nil
		e.KeyUngrouped(key)
		return
	}
	if e.mode == ModeVisual || e.mode == ModeCommand { // not reachable by the keys above; stay safe
		e.mc = nil
		e.KeyUngrouped(key)
		return
	}
	grouped := e.buf.groupDepth == 0
	if grouped {
		e.buf.BeginUndoGroup()
	}
	cs := make([]mcur, 0, 1+len(e.mc.extra))
	cs = append(cs, e.mc.extra...)
	cs = append(cs, mcur{pos: e.buf.cursor, want: e.buf.wantCol, primary: true})
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].pos.before(cs[j].pos) })

	saved := *e
	seq0 := e.buf.seq
	for k := len(cs) - 1; k >= 0; k-- {
		if cs[k].dead {
			continue
		}
		*e = saved
		e.inMulti = true
		e.buf.cursor, e.buf.wantCol = cs[k].pos, cs[k].want
		if cs[k].hasReg {
			e.register, e.registerLW = cs[k].reg, cs[k].regLW
		}
		before := cs[k].pos
		seq := e.buf.seq
		oldText := strings.Join(e.buf.lines, "\n")
		e.KeyUngrouped(key)
		after := e.buf.cursor
		if e.buf.seq != seq {
			e.mcShift(cs, k, oldText, before, after)
		}
		cs[k].pos, cs[k].want = e.buf.cursor, e.buf.wantCol
		cs[k].reg, cs[k].regLW, cs[k].hasReg = e.register, e.registerLW, true
	}
	e.inMulti = false
	changed := e.buf.seq != seq0
	if grouped {
		e.buf.EndUndoGroup()
	}

	// Rebuild: dead cursors are gone, coinciding ones merge.
	mc := e.mc
	var live []mcur
	for _, c := range cs {
		if c.dead {
			continue
		}
		c.pos = e.buf.clampPos(c.pos)
		live = append(live, c)
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].pos.before(live[j].pos) })
	merged := live[:0]
	for _, c := range live {
		if n := len(merged); n > 0 && merged[n-1].pos == c.pos {
			merged[n-1].primary = merged[n-1].primary || c.primary
			continue
		}
		merged = append(merged, c)
	}
	pi := -1
	for i, c := range merged {
		if c.primary {
			pi = i
		}
	}
	if pi < 0 {
		pi = 0
	}
	e.buf.cursor, e.buf.wantCol = merged[pi].pos, merged[pi].want
	if merged[pi].hasReg {
		e.register, e.registerLW = merged[pi].reg, merged[pi].regLW
	}
	mc.extra = mc.extra[:0]
	for i, c := range merged {
		if i != pi {
			c.primary = false
			mc.extra = append(mc.extra, c)
		}
	}
	if changed {
		mc.needle, mc.skipped = "", nil // the matches moved
	}
	if len(mc.extra) == 0 {
		e.mc = nil
	}
}

// mcShift carries every other cursor across the edit cursor k just made,
// found by diffing the text before and after. Cursors inside the replaced
// span are swallowed (they merge into the edit).
func (e *Editor) mcShift(cs []mcur, k int, oldText string, before, after Pos) {
	newText := strings.Join(e.buf.lines, "\n")
	if oldText == newText {
		return
	}
	// Common prefix, bounded by where the cursor stood and ended so repeated
	// characters resolve toward the cursor.
	i := 0
	for i < len(oldText) && i < len(newText) && oldText[i] == newText[i] {
		i++
	}
	i = min(i, offsetOf(oldText, before))
	if after.Line == before.Line {
		i = min(i, offsetOf(newText, e.buf.clampPos(after)))
	}
	for i > 0 && i < len(oldText) && !utf8.RuneStart(oldText[i]) {
		i--
	}
	j := 0
	for j < len(oldText)-i && j < len(newText)-i && oldText[len(oldText)-1-j] == newText[len(newText)-1-j] {
		j++
	}
	for j > 0 && !utf8.RuneStart(oldText[len(oldText)-j]) {
		j--
	}
	start := posAt(oldText, i)
	zOld := posAt(oldText, len(oldText)-j)
	zNew := posAt(newText, len(newText)-j)
	for n := range cs {
		if n == k || cs[n].dead {
			continue
		}
		q := cs[n].pos
		switch {
		case q.before(start):
		case q.before(zOld):
			cs[n].dead = true
			if cs[n].primary {
				cs[k].primary, cs[n].primary = true, false
			}
		case q.Line == zOld.Line:
			cs[n].pos = Pos{Line: zNew.Line, Col: zNew.Col + q.Col - zOld.Col}
		default:
			cs[n].pos = Pos{Line: q.Line + zNew.Line - zOld.Line, Col: q.Col}
		}
	}
}

// offsetOf is the byte offset of p in text (lines joined by \n).
func offsetOf(text string, p Pos) int {
	off := 0
	for l := 0; l < p.Line; l++ {
		n := strings.IndexByte(text[off:], '\n')
		if n < 0 {
			return len(text)
		}
		off += n + 1
	}
	rest := text[off:]
	if n := strings.IndexByte(rest, '\n'); n >= 0 {
		rest = rest[:n]
	}
	cut := 0
	for c := 0; c < p.Col && cut < len(rest); c++ {
		_, w := utf8.DecodeRuneInString(rest[cut:])
		cut += w
	}
	return off + cut
}

// posAt is the position of byte offset off in text.
func posAt(text string, off int) Pos {
	head := text[:off]
	line := strings.Count(head, "\n")
	if i := strings.LastIndexByte(head, '\n'); i >= 0 {
		head = head[i+1:]
	}
	return Pos{Line: line, Col: utf8.RuneCountInString(head)}
}

// mcCommand runs ":cursor next|skip|above|below|off" for terminals that
// swallow the keys.
func (e *Editor) mcCommand(arg string) {
	switch arg {
	case "next":
		e.AddCursorNext()
	case "skip":
		e.SkipCursor()
	case "above":
		e.AddCursorVertical(-1)
	case "below":
		e.AddCursorVertical(1)
	case "off":
		e.CollapseCursors()
	default:
		e.message = "usage: :cursor next|skip|above|below|off"
	}
}
