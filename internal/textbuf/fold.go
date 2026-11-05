package textbuf

import (
	"fmt"
	"sort"
	"strconv"
)

// Code folding (F-176). Folds are a view concept: the text, undo, saves
// and every position stay on real lines. The surface supplies the ranges
// (the language server's, or an indentation fallback) and the Buffer keeps
// which of them are closed. A closed fold shows its first line and hides
// the rest; j/k step over it as one line, linewise operators cover all of
// it, and anything that puts a cursor inside it (a search, a jump, an
// edit, insert mode) opens it.

// FoldRange is a foldable span: lines Start..End inclusive, End > Start.
// The first line stays visible when the fold is closed.
type FoldRange struct{ Start, End int }

// Fold is a FoldRange with its state, for rendering.
type Fold struct {
	Start, End int
	Closed     bool
}

type foldState struct {
	ranges []FoldRange  // sorted by Start, properly nested, one per Start
	closed map[int]bool // by Start
	source string       // where the ranges come from, named in messages
	seq    uint64       // buffer version the ranges describe
	snap   []string     // the lines at seq: edits are mapped by diffing against them
	spans  []FoldRange  // outermost closed folds, derived; nil = stale
	spansV bool
}

// normalizeFolds sorts, drops folds that cannot hold a hidden line or fall
// outside the buffer, keeps the largest fold per start, and drops any that
// cross an enclosing fold.
func normalizeFolds(rs []FoldRange, n int) []FoldRange {
	cp := make([]FoldRange, 0, len(rs))
	for _, r := range rs {
		if r.Start >= 0 && r.End > r.Start && r.End < n {
			cp = append(cp, r)
		}
	}
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Start != cp[j].Start {
			return cp[i].Start < cp[j].Start
		}
		return cp[i].End > cp[j].End
	})
	out := make([]FoldRange, 0, len(cp))
	var stack []FoldRange
	for _, r := range cp {
		if len(out) > 0 && out[len(out)-1].Start == r.Start {
			continue // the larger fold of this start came first
		}
		for len(stack) > 0 && stack[len(stack)-1].End < r.Start {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 && r.End > stack[len(stack)-1].End {
			continue // crosses its parent
		}
		out = append(out, r)
		stack = append(stack, r)
	}
	return out
}

// SetFoldRanges installs the foldable ranges and where they came from.
// Folds that were closed stay closed when a range still starts on their
// line.
func (b *Buffer) SetFoldRanges(rs []FoldRange, source string) {
	b.syncFolds()
	f := &b.fold
	f.ranges = normalizeFolds(rs, len(b.lines))
	f.source = source
	if len(f.closed) > 0 {
		keep := map[int]bool{}
		for _, r := range f.ranges {
			if f.closed[r.Start] {
				keep[r.Start] = true
			}
		}
		f.closed = keep
	}
	f.spansV = false
	b.foldSnapshot()
}

func (b *Buffer) foldSnapshot() {
	f := &b.fold
	f.seq = b.seq
	if len(f.ranges) == 0 {
		f.snap = nil
		return
	}
	f.snap = append(f.snap[:0], b.lines...)
}

// FoldSource names where the current ranges come from ("" = none set).
func (b *Buffer) FoldSource() string { return b.fold.source }

// Folds lists the ranges with their state, in document order.
func (b *Buffer) Folds() []Fold {
	b.syncFolds()
	out := make([]Fold, len(b.fold.ranges))
	for i, r := range b.fold.ranges {
		out[i] = Fold{Start: r.Start, End: r.End, Closed: b.fold.closed[r.Start]}
	}
	return out
}

// HasClosedFolds is the cheap check the hot paths use.
func (b *Buffer) HasClosedFolds() bool { return len(b.fold.closed) > 0 }

// syncFolds carries the ranges and closed starts across an edit. The edit
// is found by diffing the lines against the snapshot: a common prefix and
// suffix bound the changed region; lines after it shift by the length
// change, lines inside it are kept only when the region kept its size. A
// closed fold that hides a changed line is opened.
func (b *Buffer) syncFolds() {
	f := &b.fold
	if f.seq == b.seq {
		return
	}
	if len(f.ranges) == 0 {
		f.seq = b.seq
		f.snap = nil
		return
	}
	old, cur := f.snap, b.lines
	n, m := len(old), len(cur)
	p := 0
	for p < n && p < m && old[p] == cur[p] {
		p++
	}
	s := 0
	for s < n-p && s < m-p && old[n-1-s] == cur[m-1-s] {
		s++
	}
	delta := m - n
	oldEnd, newEnd := n-s, m-s // changed region: old [p,oldEnd), new [p,newEnd)
	mapStart := func(l int) int {
		switch {
		case l < p:
			return l
		case l >= oldEnd:
			return l + delta
		case delta == 0:
			return l
		case l == p && newEnd > p:
			return l
		}
		return -1
	}
	mapEnd := func(l int) int {
		switch {
		case l < p:
			return l
		case l >= oldEnd:
			return l + delta
		case delta == 0:
			return l
		}
		return max(newEnd-1, p)
	}
	var out []FoldRange
	closed := map[int]bool{}
	for _, r := range f.ranges {
		st, en := mapStart(r.Start), mapEnd(r.End)
		if st < 0 || en <= st || en >= m {
			continue
		}
		out = append(out, FoldRange{st, en})
		if f.closed[r.Start] {
			// Did the edit touch the hidden lines r.Start+1..r.End?
			touched := r.Start+1 < oldEnd && r.End >= p
			if oldEnd == p { // a pure insertion between two lines
				touched = p > r.Start && p <= r.End
			}
			if touched {
				continue // opened by the edit
			}
			closed[st] = true
		}
	}
	f.ranges = normalizeFolds(out, m)
	if len(closed) == 0 {
		closed = nil
	}
	f.closed = closed
	f.spansV = false
	b.foldSnapshot()
}

// closedSpans are the outermost closed folds: what hides lines.
func (b *Buffer) closedSpans() []FoldRange {
	b.syncFolds()
	f := &b.fold
	if f.spansV {
		return f.spans
	}
	f.spans = f.spans[:0]
	end := -1
	for _, r := range f.ranges {
		if r.Start > end && f.closed[r.Start] {
			f.spans = append(f.spans, r)
			end = r.End
		}
	}
	f.spansV = true
	return f.spans
}

// ClosedFoldAt reports the outermost closed fold holding line, its first
// line included.
func (b *Buffer) ClosedFoldAt(line int) (lo, hi int, ok bool) {
	if len(b.fold.closed) == 0 && b.fold.seq == b.seq {
		return 0, 0, false
	}
	sp := b.closedSpans()
	i := sort.Search(len(sp), func(i int) bool { return sp[i].End >= line })
	if i < len(sp) && sp[i].Start <= line {
		return sp[i].Start, sp[i].End, true
	}
	return 0, 0, false
}

// FoldHidden reports whether line is hidden inside a closed fold.
func (b *Buffer) FoldHidden(line int) bool {
	lo, _, ok := b.ClosedFoldAt(line)
	return ok && line > lo
}

// snapFold moves a cursor that sits inside a closed fold: backward to its
// first line, or (forward) to the line after it.
func (b *Buffer) snapFold(forward bool) {
	lo, hi, ok := b.ClosedFoldAt(b.cursor.Line)
	if !ok || b.cursor.Line == lo {
		return
	}
	if forward && hi+1 < len(b.lines) {
		b.cursor = Pos{Line: hi + 1}
		b.wantCol = 0
		return
	}
	b.cursor.Line = lo
	b.applyWantCol()
}

// foldLines counts n visible lines from line (a closed fold is one line)
// and returns the first line of the group and the line after it.
func (b *Buffer) foldLines(line, n int) (lo, end int) {
	if l, _, ok := b.ClosedFoldAt(line); ok {
		line = l
	}
	lo, end = line, line
	for i := 0; i < max(n, 1) && end < len(b.lines); i++ {
		if _, hi, ok := b.ClosedFoldAt(end); ok {
			end = hi + 1
		} else {
			end++
		}
	}
	return lo, min(end, len(b.lines))
}

// foldWiden extends lines lo..hi (inclusive) over any closed fold they
// start or end in.
func (b *Buffer) foldWiden(lo, hi int) (int, int) {
	if l, _, ok := b.ClosedFoldAt(lo); ok {
		lo = l
	}
	if _, h, ok := b.ClosedFoldAt(hi); ok {
		hi = h
	}
	return lo, hi
}

// containing lists the folds holding line, outermost first.
func (b *Buffer) foldsContaining(line int) []FoldRange {
	var out []FoldRange
	for _, r := range b.fold.ranges {
		if r.Start > line {
			break
		}
		if r.End >= line {
			out = append(out, r)
		}
	}
	return out
}

func (b *Buffer) setClosed(r FoldRange, closed bool) {
	f := &b.fold
	if closed {
		if f.closed == nil {
			f.closed = map[int]bool{}
		}
		f.closed[r.Start] = true
	} else {
		delete(f.closed, r.Start)
	}
	f.spansV = false
}

// foldReveal opens every closed fold that hides line.
func (b *Buffer) foldReveal(line int) {
	if len(b.fold.closed) == 0 {
		return
	}
	for _, r := range b.foldsContaining(line) {
		if r.Start < line && b.fold.closed[r.Start] {
			b.setClosed(r, false)
		}
	}
}

// foldOpenAt opens every closed fold holding line, first line included
// (insert mode edits the text, so it must be showing).
func (b *Buffer) foldOpenAt(line int) {
	if len(b.fold.closed) == 0 {
		return
	}
	for _, r := range b.foldsContaining(line) {
		if b.fold.closed[r.Start] {
			b.setClosed(r, false)
		}
	}
}

// RevealFolds opens the folds hiding any cursor; the surface calls it
// before drawing so a jump it made itself (a click, go to definition)
// never lands on a hidden line.
func (e *Editor) RevealFolds() {
	e.buf.syncFolds()
	if !e.buf.HasClosedFolds() {
		return
	}
	e.buf.foldReveal(e.buf.cursor.Line)
	if e.mc != nil {
		for _, c := range e.mc.extra {
			e.buf.foldReveal(c.pos.Line)
		}
	}
}

// foldAfterKey keeps the fold invariants after any key: edits are mapped,
// insert mode opens the fold under the cursor, and a cursor that landed
// inside a closed fold (a search, a jump) opens it.
func (e *Editor) foldAfterKey() {
	b := e.buf
	b.syncFolds()
	if !b.HasClosedFolds() {
		return
	}
	if e.mode == ModeInsert {
		b.foldOpenAt(b.cursor.Line)
	}
	e.RevealFolds()
}

// Fold keys. The surface sends the z-chord whole.
func isFoldKey(key string) bool {
	switch key {
	case "za", "zc", "zo", "zA", "zC", "zO", "zM", "zR", "zj", "zk":
		return true
	}
	return false
}

// IsFoldKey reports whether key is one of the z fold commands.
func IsFoldKey(key string) bool { return isFoldKey(key) }

// foldKey runs a fold command outside an operator; it reports whether it
// consumed the key.
func (e *Editor) foldKey(key string) bool {
	if !isFoldKey(key) || (e.mode != ModeNormal && e.mode != ModeVisual) || e.pendingOp != opNone {
		return false
	}
	count := e.count()
	e.cntDigits = nil
	b := e.buf
	b.syncFolds()
	if len(b.fold.ranges) == 0 && key != "zM" && key != "zR" {
		e.message = e.foldNote("no folds")
		return true
	}
	line := b.cursor.Line
	switch key {
	case "zj", "zk":
		t, ok := b.foldTarget(line, key == "zj", count)
		if !ok {
			e.message = e.foldNote("no more folds")
			return true
		}
		b.cursor.Line = t
		b.snapFold(false)
		b.applyWantCol()
		return true
	case "za":
		if _, _, ok := b.ClosedFoldAt(line); ok {
			e.foldOpen(false, count)
		} else {
			e.foldClose(false, count)
		}
	case "zo":
		e.foldOpen(false, count)
	case "zO":
		e.foldOpen(true, 0)
	case "zc":
		e.foldClose(false, count)
	case "zC":
		e.foldClose(true, 0)
	case "zA":
		if _, _, ok := b.ClosedFoldAt(line); ok {
			e.foldOpen(true, 0)
		} else {
			e.foldClose(true, 0)
		}
	case "zM":
		for _, r := range b.fold.ranges {
			b.setClosed(r, true)
		}
		e.message = e.foldNote(strconv.Itoa(len(b.fold.ranges)) + " folds closed")
	case "zR":
		b.fold.closed = nil
		b.fold.spansV = false
		e.message = e.foldNote("all folds open")
	}
	e.foldCollapseCursors()
	return true
}

func (e *Editor) foldNote(what string) string {
	src := e.buf.fold.source
	if src == "" {
		return what
	}
	return what + " · folds from " + src
}

// foldClose closes the innermost open fold around the cursor (count
// levels, or every enclosing fold when all).
func (e *Editor) foldClose(all bool, count int) {
	b := e.buf
	cs := b.foldsContaining(b.cursor.Line)
	if len(cs) == 0 {
		e.message = e.foldNote("no fold here")
		return
	}
	closedN, hidden := 0, 0
	for i := len(cs) - 1; i >= 0 && (all || closedN < max(count, 1)); i-- {
		if !b.fold.closed[cs[i].Start] {
			b.setClosed(cs[i], true)
			closedN++
			hidden = cs[i].End - cs[i].Start
		}
	}
	if closedN == 0 {
		e.message = e.foldNote("already closed")
		return
	}
	e.message = e.foldNote(fmt.Sprintf("fold closed, %d lines", hidden))
}

// foldOpen opens the outermost closed fold around the cursor (count
// levels, or every enclosing fold when all).
func (e *Editor) foldOpen(all bool, count int) {
	b := e.buf
	cs := b.foldsContaining(b.cursor.Line)
	openedN := 0
	for _, r := range cs {
		if b.fold.closed[r.Start] && (all || openedN < max(count, 1)) {
			b.setClosed(r, false)
			openedN++
		}
	}
	if openedN == 0 {
		e.message = e.foldNote("no closed fold here")
		return
	}
	e.message = e.foldNote("fold opened")
}

// foldCollapseCursors moves every cursor that a closing left inside a
// closed fold onto its first line; cursors that meet merge. (Opening is
// the other half: a cursor jumping into a fold opens it, see RevealFolds.)
func (e *Editor) foldCollapseCursors() {
	b := e.buf
	b.snapFold(false)
	if e.mc == nil {
		return
	}
	seen := map[Pos]bool{b.cursor: true}
	kept := e.mc.extra[:0]
	for _, c := range e.mc.extra {
		if lo, _, ok := b.ClosedFoldAt(c.pos.Line); ok && c.pos.Line > lo {
			c.pos = Pos{Line: lo, Col: min(c.pos.Col, len(b.runes(lo)))}
		}
		if !seen[c.pos] {
			seen[c.pos] = true
			kept = append(kept, c)
		}
	}
	e.mc.extra = kept
	if len(kept) == 0 {
		e.mc = nil
	}
}

// foldTarget finds the count-th fold start after line (zj) or fold end
// before it (zk), a closed fold counting as one fold.
func (b *Buffer) foldTarget(line int, down bool, count int) (int, bool) {
	var cands []int
	hidden := -1
	for _, r := range b.fold.ranges {
		if r.Start <= hidden {
			continue // inside a closed fold
		}
		if down {
			cands = append(cands, r.Start)
		} else {
			cands = append(cands, r.End)
		}
		if b.fold.closed[r.Start] {
			hidden = r.End
		}
	}
	sort.Ints(cands)
	cur := line
	if lo, hi, ok := b.ClosedFoldAt(line); ok {
		if down {
			cur = hi
		} else {
			cur = lo
		}
	}
	t, ok := cur, false
	for i := 0; i < max(count, 1); i++ {
		found := false
		if down {
			for _, c := range cands {
				if c > t {
					t, found = c, true
					break
				}
			}
		} else {
			for j := len(cands) - 1; j >= 0; j-- {
				if cands[j] < t {
					t, found = cands[j], true
					break
				}
			}
		}
		if !found {
			break
		}
		ok = true
	}
	return t, ok
}

// SetFoldSource renames where the ranges come from without touching them
// (the surface does it while it waits for a server's answer).
func (b *Buffer) SetFoldSource(s string) { b.fold.source = s }

// foldOperator is zj/zk after d, c, y or gc: a linewise span from the
// cursor line to the target (closed folds included whole).
func (e *Editor) foldOperator(key string, count int) {
	b := e.buf
	b.syncFolds()
	t, ok := b.foldTarget(b.cursor.Line, key == "zj", count)
	if !ok {
		e.clearPending()
		e.message = e.foldNote("no more folds")
		return
	}
	lo, hi := b.cursor.Line, t
	if lo > hi {
		lo, hi = hi, lo
	}
	lo, hi = b.foldWiden(lo, hi)
	e.runOperator(e.pendingOp, Pos{Line: lo}, Pos{Line: hi + 1}, true)
	e.clearPending()
}
