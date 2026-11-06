package editor

import (
	"strconv"
	"time"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Code folding (F-176). The fold state lives in the buffer (textbuf), so
// every pane showing a tab shares it with the cursor that j/k and the
// operators move; this file feeds it ranges and draws it.
//
// Ranges come from the language server's textDocument/foldingRange when it
// offers one. Otherwise an indentation fallback is used and says so (the
// z-command messages end "folds from indentation (...)" with the reason).
// A server that does offer folding but has not answered yet shows no
// folds, labelled "waiting for the server"; the fallback is never
// substituted silently (ADR-0011).

// foldDebounce is how long the text must sit still before ranges are
// requested again.
var foldDebounce = 250 * time.Millisecond

// foldSrc is one tab's request bookkeeping.
type foldSrc struct {
	have    bool   // an answer for some version of the text is installed
	haveSeq uint64 // the buffer version the installed answer describes
	want    bool
	wantSeq uint64
	gen     int    // request generation; an older answer is dropped
	failed  string // the server's error: the fallback takes over, named
	indSeq  uint64 // buffer version the indentation ranges describe
	indSrc  string // source label they were installed under
}

// foldSync makes sure tab's buffer holds fold ranges for its current text.
// Called from render.
func (m *Model) foldSync(tab *bufTab) {
	e := tab.ed
	if e == nil {
		return
	}
	b := e.Buffer()
	seq := b.Seq()
	st := &tab.fold
	var c *lsp.Client
	if tab.path != "" {
		c = m.clientFor(tab.path)
	}
	if c != nil && c.CanFoldingRange() && st.failed == "" {
		name := serverLabel(c)
		if !st.have {
			b.SetFoldSource(name + ", waiting for the server")
		} else {
			b.SetFoldSource(name)
		}
		m.foldAsk(tab, seq)
		return
	}
	var src string
	switch {
	case c == nil:
		src = "indentation (no language server for this file)"
	case st.failed != "":
		src = "indentation (" + serverLabel(c) + " folding failed: " + st.failed + ")"
	default:
		src = "indentation (" + serverLabel(c) + " offers no folding ranges)"
	}
	if st.indSeq == seq && st.indSrc == src && len(b.Folds()) > 0 {
		return
	}
	rs := indentFolds(b, tabStop)
	b.SetFoldRanges(rs, src)
	st.indSeq, st.indSrc = seq, src
	if len(rs) == 0 {
		st.indSeq = 0 // recheck next time: cheap, and a later edit may add one
	}
}

func serverLabel(c *lsp.Client) string {
	if n := c.ServerName(); n != "" {
		return n
	}
	return "the language server"
}

// indentFolds is the fallback: a line followed by deeper-indented lines
// folds over them (blank lines inside belong to the fold, trailing blanks
// do not). Tabs count as tabStop columns.
func indentFolds(b *textbuf.Buffer, tabW int) []textbuf.FoldRange {
	type open struct {
		start, ind int
		child      bool
	}
	var out []textbuf.FoldRange
	var stack []open
	last := -1 // the last non-blank line seen
	n := b.LineCount()
	for l := 0; l < n; l++ {
		ind, blank := indentOf(b.Line(l), tabW)
		if blank {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].ind >= ind {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.child && last > top.start {
				out = append(out, textbuf.FoldRange{Start: top.start, End: last})
			}
		}
		if len(stack) > 0 {
			stack[len(stack)-1].child = true
		}
		stack = append(stack, open{start: l, ind: ind})
		last = l
	}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top.child && last > top.start {
			out = append(out, textbuf.FoldRange{Start: top.start, End: last})
		}
	}
	return out
}

// indentOf is a line's indentation in columns and whether it is blank.
func indentOf(line string, tabW int) (int, bool) {
	w := 0
	for _, r := range line {
		switch r {
		case ' ':
			w++
		case '\t':
			w += tabW - w%tabW
		default:
			return w, false
		}
	}
	return 0, true
}

// foldAsk requests the server's ranges for seq after the debounce.
func (m *Model) foldAsk(tab *bufTab, seq uint64) {
	st := &tab.fold
	if m.termMsgs == nil || tab.path == "" {
		return
	}
	if (st.have && st.haveSeq == seq) || (st.want && st.wantSeq == seq) {
		return
	}
	st.gen++
	st.want, st.wantSeq = true, seq
	gen, ch, path, wait := st.gen, m.termMsgs, tab.path, foldDebounce
	go func() {
		time.Sleep(wait)
		select {
		case ch <- teaMsg{kind: lspMsgFoldDue, note: path, sigSeq: gen}:
		default:
		}
	}()
}

// applyFoldMsg folds the two async steps: the debounce elapsing (send the
// request with the server holding the current text) and the answer.
func (m *Model) applyFoldMsg(msg teaMsg) {
	var tab *bufTab
	for _, t := range m.bufs {
		if t.ed != nil && t.path == msg.note {
			tab = t
		}
	}
	if tab == nil || msg.sigSeq != tab.fold.gen {
		return // closed, or overtaken by a newer request
	}
	st := &tab.fold
	ed := tab.ed
	switch msg.kind {
	case lspMsgFoldDue:
		c := m.clientFor(msg.note)
		if c == nil || ed.Buffer().Seq() != st.wantSeq {
			st.want = false // the text moved on; the next render asks again
			return
		}
		if text := ed.Buffer().Text(); m.lspSent[tab.vp] != text {
			if _, opened := m.lspSent[tab.vp]; opened {
				_ = c.DidChange(tab.path, text)
				m.lspSent[tab.vp] = text
			}
		}
		gen, seq, path, ch := st.gen, st.wantSeq, msg.note, m.termMsgs
		go func() {
			rs, err := c.FoldingRanges(path)
			out := teaMsg{kind: lspMsgFold, note: path, sigSeq: gen, bufSeq: seq, folds: rs, hoverOK: err == nil}
			if err != nil {
				out.hoverText = err.Error()
			}
			select {
			case ch <- out:
			default:
			}
		}()
	case lspMsgFold:
		st.want = false
		if !msg.hoverOK {
			st.failed = msg.hoverText
			ed.SetMessage("lsp: folding: " + msg.hoverText + " (using indentation folds)")
			return
		}
		b := ed.Buffer()
		if msg.bufSeq != b.Seq() {
			return // typed since; the next render asks again
		}
		rs := make([]textbuf.FoldRange, 0, len(msg.folds))
		for _, f := range msg.folds {
			rs = append(rs, textbuf.FoldRange{Start: f.StartLine, End: f.EndLine})
		}
		b.SetFoldRanges(rs, serverLabel(m.clientFor(msg.note)))
		st.have, st.haveSeq = true, msg.bufSeq
	}
}

// windowLines picks the lines a view draws so the cursor line sits near
// the middle of rows screen rows, filling to the bottom at the end of the
// buffer. cost is the screen rows a line takes; closed folds hide the
// lines inside them.
func windowLines(b *textbuf.Buffer, cur, rows int, cost func(l int) int) []int {
	above := []int{} // descending from just above cur
	used, top := 0, cur
	for {
		p := prevVisible(b, top)
		if p < 0 || used+cost(p) > rows/2 {
			break
		}
		above, top, used = append(above, p), p, used+cost(p)
	}
	var below []int
	for l := cur; l < b.LineCount() && used < rows; l = nextVisible(b, l) {
		below = append(below, l)
		used += cost(l)
	}
	for { // short of the end: use the room above
		p := prevVisible(b, top)
		if p < 0 || used+cost(p) > rows {
			break
		}
		above, top, used = append(above, p), p, used+cost(p)
	}
	out := make([]int, 0, len(above)+len(below))
	for i := len(above) - 1; i >= 0; i-- {
		out = append(out, above[i])
	}
	return append(out, below...)
}

// prevVisible is the drawn line before l, or -1.
func prevVisible(b *textbuf.Buffer, l int) int {
	l--
	if l < 0 {
		return -1
	}
	if lo, _, ok := b.ClosedFoldAt(l); ok {
		l = lo
	}
	return l
}

// nextVisible is the drawn line after l (LineCount at the end).
func nextVisible(b *textbuf.Buffer, l int) int {
	if _, hi, ok := b.ClosedFoldAt(l); ok {
		l = hi
	}
	return l + 1
}

// foldMark is what the gutter shows on line l: the fold glyph when a fold
// starts there, "" otherwise. Open folds show their glyph on the cursor
// line only, so a screen of code is not a column of arrows (closed ones
// always show, they are what hides lines).
func foldMark(folds map[int]bool, l, cur int) string {
	closed, ok := folds[l]
	switch {
	case !ok:
		return ""
	case closed:
		return theme.GlyphFoldClosed
	case l == cur:
		return theme.GlyphFoldOpen
	}
	return ""
}

// foldStarts maps each fold's first line to whether it is closed.
func foldStarts(b *textbuf.Buffer) map[int]bool {
	fs := b.Folds()
	if len(fs) == 0 {
		return nil
	}
	out := make(map[int]bool, len(fs))
	for _, f := range fs {
		out[f.Start] = f.Closed
	}
	return out
}

// foldTail is the dim text after a closed fold's first line.
func foldTail(hidden int) string {
	unit := " lines"
	if hidden == 1 {
		unit = " line"
	}
	return theme.TextDim().Render(" " + theme.GlyphFoldMore + " " + strconv.Itoa(hidden) + unit)
}

// foldZKey handles the second key of a z command. It reports whether it
// consumed key (a z-sequence that is none is swallowed, like g's).
func (m *Model) foldZKey(e *textbuf.Editor, key string) {
	if key == "esc" {
		return
	}
	if textbuf.IsFoldKey("z" + key) {
		e.Key("z" + key)
		return
	}
	e.SetMessage("z" + key + ": not a fold command (za zc zo zA zC zO zM zR zj zk)")
}

// foldLineAt maps a surface row of the drawn window to its buffer line.
func (g bufGeom) foldLineAt(row int) (int, bool) {
	switch {
	case g.rowLine != nil:
		if row < 0 || row >= len(g.rowLine) {
			return 0, false
		}
		return g.rowLine[row], true
	case g.lines != nil:
		if row < 0 || row >= len(g.lines) {
			return 0, false
		}
		return g.lines[row], true
	}
	return g.top + row, true
}

// foldClick toggles the fold whose gutter marker is at (x, y): the cell
// just before the text, where ▸ and the cursor line's ▾ are drawn. A click
// anywhere else in the gutter places the cursor.
func (m *Model) foldClick(x, y int) bool {
	g := m.bufGeom
	e := m.active()
	if !g.ok || e == nil || m.mode != modeNav || m.reviewOpen || x != g.ox+g.textX-1 || y < g.oy || y >= g.oy+g.rows {
		return false
	}
	line, ok := g.foldLineAt(y - g.oy)
	if !ok {
		return false
	}
	b := e.Buffer()
	if line >= b.LineCount() {
		return false
	}
	if foldMark(foldStarts(b), line, b.Cursor().Line) == "" {
		return false // no marker is drawn there
	}
	if e.Mode() == textbuf.ModeVisual {
		e.Key("esc")
	}
	b.SetCursor(textbuf.Pos{Line: line, Col: b.Cursor().Col})
	e.Key("za")
	m.bufFocus = true
	return true
}
