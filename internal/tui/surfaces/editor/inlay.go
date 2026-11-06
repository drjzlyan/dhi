package editor

import (
	"sort"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/mattn/go-runewidth"
)

// Inlay hints (F-169): the server's short labels (an inferred type after
// a variable, a parameter name before an argument) drawn dimmed between
// the buffer's own characters. A hint is a virtual cell run: it takes
// room on screen but is not text, so the cursor, selections, clicks and
// soft wrap (F-136) keep addressing buffer columns only.
//
// Hints for the visible window (plus a screen of margin) are requested
// after a short debounce, off the UI goroutine, and cached per buffer
// version; an answer for an older request or version is dropped.

// inlayDebounce is how long the window must sit still before hints are
// requested (edits and scrolling both restart it).
var inlayDebounce = 250 * time.Millisecond

// inlayState is one file's hint cache and its in-flight request.
type inlayState struct {
	have     bool
	bufSeq   uint64 // buffer version the cached hints answer
	lines    int    // line count then (a stale cache is only trusted when it matches)
	from, to int    // lines [from,to) the cache covers
	byLine   map[int][]lsp.InlayHint
	gen      int // request generation; an answer from an older one is dropped
	want     bool
	wantSeq  uint64
	wantFrom int
	wantTo   int
	errNoted bool
}

// inlayCell is one hint resolved against its line: where it goes and what
// it looks like. idx is the expanded-line rune it precedes; cell the
// display cells of plain text before it.
type inlayCell struct {
	idx, cell, w int
	text         string
}

// inlayEnabled is the effective setting: a session :set inlayhints|
// noinlayhints beats editor.inlay_hints.
func (m *Model) inlayEnabled(p Prefs) bool {
	if m.inlayOverride != nil {
		return *m.inlayOverride
	}
	return p.InlayHints
}

// inlayCommand handles :set inlayhints and :set noinlayhints (this
// session only; the setting itself lives in Settings).
func (m *Model) inlayCommand(cmd string) (string, bool) {
	var on bool
	switch strings.TrimSpace(cmd) {
	case "set inlayhints":
		on = true
	case "set noinlayhints":
		on = false
	default:
		return "", false
	}
	m.inlayOverride = &on
	if !on {
		m.inlayConfigureAll(false)
		return "inlay hints: off", true
	}
	if c := m.lspClient(); c != nil && !c.CanInlayHints() {
		return "inlay hints: on, but " + c.ServerName() + " does not provide them", true
	}
	return "inlay hints: on", true
}

// inlayConfigure tells c whether to emit hints, once per change (gopls
// emits none until told).
func (m *Model) inlayConfigure(c *lsp.Client, on bool) {
	if m.inlayCfg == nil {
		m.inlayCfg = map[*lsp.Client]bool{}
	}
	prev, known := m.inlayCfg[c]
	if (known && prev == on) || (!known && !on) {
		return // unchanged, or never turned on: nothing to undo
	}
	m.inlayCfg[c] = on
	_ = c.SetInlayHints(on)
	m.inlayDropAll()
}

// inlayConfigureAll turns hints off on every client that was told to emit
// them.
func (m *Model) inlayConfigureAll(on bool) {
	for c, was := range m.inlayCfg {
		if was != on {
			m.inlayConfigure(c, on)
		}
	}
	if !on {
		m.inlayDropAll()
	}
}

// inlayDropAll forgets every cached hint.
func (m *Model) inlayDropAll() {
	for _, st := range m.inlay {
		st.have, st.byLine, st.want, st.gen = false, nil, false, st.gen+1
	}
}

// inlayWant asks for hints covering lines [top,end) of e when the cache
// does not already hold them for this buffer version. Called from render;
// the request itself goes out after the debounce.
func (m *Model) inlayWant(e *textbuf.Editor, top, end int) {
	path := e.Path()
	if path == "" || m.termMsgs == nil {
		return
	}
	c := m.clientFor(path)
	if c == nil || !c.CanInlayHints() {
		return
	}
	m.inlayConfigure(c, true)
	if m.inlay == nil {
		m.inlay = map[string]*inlayState{}
	}
	st := m.inlay[path]
	if st == nil {
		st = &inlayState{}
		m.inlay[path] = st
	}
	seq := e.Buffer().Seq()
	if st.have && st.bufSeq == seq && st.from <= top && end <= st.to {
		return
	}
	if st.want && st.wantSeq == seq && st.wantFrom <= top && end <= st.wantTo {
		return
	}
	span := end - top
	st.gen++
	st.want, st.wantSeq = true, seq
	st.wantFrom, st.wantTo = max(top-span, 0), min(end+span, e.Buffer().LineCount())
	gen, ch, wait := st.gen, m.termMsgs, inlayDebounce
	go func() {
		time.Sleep(wait)
		select {
		case ch <- teaMsg{kind: lspMsgInlayDue, note: path, sigSeq: gen}:
		default:
		}
	}()
}

// applyInlayMsg folds the two async steps: the debounce elapsing (send
// the request now, with the server holding the current text) and the
// answer arriving.
func (m *Model) applyInlayMsg(msg teaMsg) {
	st := m.inlay[msg.note]
	if st == nil || msg.sigSeq != st.gen {
		return // overtaken by a newer request or a reset
	}
	var ed *textbuf.Editor
	var tab *bufTab
	for _, t := range m.bufs {
		if t.ed != nil && t.path == msg.note {
			ed, tab = t.ed, t
		}
	}
	if ed == nil {
		return
	}
	switch msg.kind {
	case lspMsgInlayDue:
		c := m.clientFor(msg.note)
		if c == nil || ed.Buffer().Seq() != st.wantSeq {
			return // the text moved on; the next render asks again
		}
		// The server must see the text the range refers to (any pane's tab,
		// not only the focused one).
		if text := ed.Buffer().Text(); m.lspSent[tab.vp] != text {
			if _, opened := m.lspSent[tab.vp]; opened {
				_ = c.DidChange(tab.path, text)
				m.lspSent[tab.vp] = text
			}
		}
		b := ed.Buffer()
		from, to := st.wantFrom, st.wantTo
		if to <= from {
			return
		}
		last := b.Line(to - 1)
		rng := lsp.Range{
			Start: lsp.Position{Line: from},
			End:   lsp.Position{Line: to - 1, Character: lsp.UTF16Col(last, len([]rune(last)))},
		}
		gen, seq, lines, path, ch := st.gen, st.wantSeq, b.LineCount(), msg.note, m.termMsgs
		go func() {
			hints, err := c.InlayHints(path, rng)
			out := teaMsg{kind: lspMsgInlay, note: path, sigSeq: gen, bufSeq: seq, inlay: hints, hoverOK: err == nil, tab: lines}
			if err != nil {
				out.hoverText = err.Error()
			}
			select {
			case ch <- out:
			default:
			}
		}()
	case lspMsgInlay:
		st.want = false
		if !msg.hoverOK {
			if !st.errNoted {
				st.errNoted = true
				ed.SetMessage("lsp: inlay hints: " + msg.hoverText)
			}
		} else {
			st.errNoted = false
		}
		st.have, st.bufSeq, st.lines = true, msg.bufSeq, msg.tab
		st.from, st.to = st.wantFrom, st.wantTo
		st.byLine = map[int][]lsp.InlayHint{}
		for _, h := range msg.inlay {
			st.byLine[h.Line] = append(st.byLine[h.Line], h)
		}
	}
}

// inlayCells resolves the cached hints of line l, in line order. nil when
// hints are off, the cache is for another version, or the line has none.
// inlayCells is every virtual cell run of line l: the server's hints (F-169)
// and, on the cursor line, ghost text (F-181), which sorts last.
func (m *Model) inlayCells(e *textbuf.Editor, l int) []inlayCell {
	cells := m.hintCells(e, l)
	if g := m.ghostCell(e, l); g != nil {
		cells = append(cells, g...)
	}
	return cells
}

// hintCells resolves the cached inlay hints of line l.
func (m *Model) hintCells(e *textbuf.Editor, l int) []inlayCell {
	if !m.inlayEnabled(m.editorPrefs()) {
		return nil
	}
	st := m.inlay[e.Path()]
	if st == nil || !st.have {
		return nil
	}
	hs := st.byLine[l]
	if len(hs) == 0 {
		return nil
	}
	b := e.Buffer()
	// An exact version is always right; after an edit the cache is kept
	// for the lines the edit cannot have moved, so typing does not make
	// every hint blink out until the next answer.
	if st.bufSeq != b.Seq() && (st.lines != b.LineCount() || l == b.Cursor().Line) {
		return nil
	}
	raw := b.Line(l)
	rs := []rune(expandTabs(raw))
	out := make([]inlayCell, 0, len(hs))
	for _, h := range hs {
		idx := min(visCol(raw, lsp.RuneCol(raw, h.Col)), len(rs))
		label := h.Label
		if h.PadLeft {
			label = " " + label
		}
		if h.PadRight {
			label += " "
		}
		out = append(out, inlayCell{idx: idx, cell: runesWidth(rs[:idx]), w: runewidth.StringWidth(label),
			text: theme.TextDim().Render(label)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].idx < out[j].idx })
	return out
}

// insertInlay puts each hint into the styled line at its display cell.
func insertInlay(text string, cells []inlayCell) string {
	var b strings.Builder
	prev, total := 0, ansi.Width(text)
	for _, c := range cells {
		at := min(c.cell, total)
		b.WriteString(ansi.Slice(text, prev, at))
		b.WriteString(c.text)
		prev = at
	}
	b.WriteString(ansi.Slice(text, prev, total))
	return b.String()
}

// hintsBefore is the display width of the hints placed before expanded
// rune idx (hints AT idx come after it).
func hintsBefore(cells []inlayCell, idx int) int {
	w := 0
	for _, c := range cells {
		if c.idx < idx {
			w += c.w
		}
	}
	return w
}

// hintsUpTo is the display width of the hints at or before idx: where a
// block cursor on rune idx sits relative to the hints that precede it.
func hintsUpTo(cells []inlayCell, idx int) int { return hintsBefore(cells, idx+1) }

// hintsBetween is the width of the hints at runes [lo,hi].
func hintsBetween(cells []inlayCell, lo, hi int) int {
	return hintsBefore(cells, hi+1) - hintsBefore(cells, lo)
}

// devirtCell maps a display cell of the line with hints back to the cell
// of the plain text under it; a cell on a hint maps to the text after it.
func devirtCell(cells []inlayCell, v int) int {
	shift := 0
	for _, c := range cells {
		start := c.cell + shift
		if v < start {
			break
		}
		if v < start+c.w {
			return c.cell
		}
		shift += c.w
	}
	return v - shift
}

// extraWidths is, per rune of a line of n runes, the hint width placed
// before it (the wrap breaker counts it into the row). nil without hints.
func extraWidths(cells []inlayCell, n int) []int {
	if len(cells) == 0 {
		return nil
	}
	extra := make([]int, n+1)
	for _, c := range cells {
		extra[min(c.idx, n)] += c.w
	}
	return extra
}
