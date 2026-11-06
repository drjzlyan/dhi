package editor

import (
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Breadcrumbs and sticky scroll (F-175): both read the cursor's position
// in the language server's document outline (the same textDocument/
// documentSymbol the :sym picker uses), cached per file and refreshed a
// moment after the text changes. Without a server there is no outline and
// nothing is guessed: the breadcrumbs show the path alone and nothing is
// pinned (ADR-0011).

// outlineDebounce is how long the buffer must sit still before the
// outline is re-requested (edits restart it).
var outlineDebounce = 300 * time.Millisecond

// stickyMax is the most header lines sticky scroll pins.
const stickyMax = 3

// outlineState is one file's cached outline and its in-flight request.
type outlineState struct {
	have     bool
	bufSeq   uint64 // buffer version the outline answers
	lines    int    // line count then
	syms     []lsp.Symbol
	gen      int // request generation; an answer from an older one is dropped
	want     bool
	wantSeq  uint64
	errNoted bool
}

// breadcrumbsEnabled is the effective setting: a session :set breadcrumbs|
// nobreadcrumbs beats editor.breadcrumbs.
func (m *Model) breadcrumbsEnabled(p Prefs) bool {
	if m.crumbOverride != nil {
		return *m.crumbOverride
	}
	return p.Breadcrumbs
}

// stickyEnabled is the effective setting: a session :set sticky|nosticky
// beats editor.sticky_scroll.
func (m *Model) stickyEnabled(p Prefs) bool {
	if m.stickyOverride != nil {
		return *m.stickyOverride
	}
	return p.StickyScroll
}

// outlineCommand handles :set [no]breadcrumbs and :set [no]sticky (this
// session only; the settings live in Settings).
func (m *Model) outlineCommand(cmd string) (string, bool) {
	switch strings.TrimSpace(cmd) {
	case "set breadcrumbs":
		on := true
		m.crumbOverride = &on
		return "breadcrumbs: on", true
	case "set nobreadcrumbs":
		on := false
		m.crumbOverride = &on
		return "breadcrumbs: off", true
	case "set sticky":
		on := true
		m.stickyOverride = &on
		return m.stickyNote("sticky scroll: on"), true
	case "set nosticky":
		on := false
		m.stickyOverride = &on
		return "sticky scroll: off", true
	}
	return "", false
}

// stickyNote names a missing server or outline when sticky scroll is
// turned on (it has nothing to pin from then).
func (m *Model) stickyNote(msg string) string {
	c := m.lspClient()
	if c == nil {
		return msg + ", but there is no language server for this file"
	}
	if !c.CanDocumentSymbols() {
		return msg + ", but " + c.ServerName() + " provides no outline"
	}
	return msg
}

// outlineWant asks for e's outline when the cache does not hold it for
// this buffer version. Called from render; the request goes out after the
// debounce, off the UI goroutine.
func (m *Model) outlineWant(e *textbuf.Editor) {
	path := e.Path()
	if path == "" || m.termMsgs == nil {
		return
	}
	c := m.clientFor(path)
	if c == nil || !c.CanDocumentSymbols() {
		return
	}
	if m.outline == nil {
		m.outline = map[string]*outlineState{}
	}
	st := m.outline[path]
	if st == nil {
		st = &outlineState{}
		m.outline[path] = st
	}
	seq := e.Buffer().Seq()
	if (st.have && st.bufSeq == seq) || (st.want && st.wantSeq == seq) {
		return
	}
	st.gen++
	st.want, st.wantSeq = true, seq
	gen, ch, wait := st.gen, m.termMsgs, outlineDebounce
	go func() {
		time.Sleep(wait)
		select {
		case ch <- teaMsg{kind: lspMsgOutlineDue, note: path, sigSeq: gen}:
		default:
		}
	}()
}

// applyOutlineMsg folds the two async steps: the debounce elapsing (send
// the request, the server holding the current text) and the answer.
func (m *Model) applyOutlineMsg(msg teaMsg) {
	st := m.outline[msg.note]
	if st == nil || msg.sigSeq != st.gen {
		return // overtaken by a newer request
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
	case lspMsgOutlineDue:
		c := m.clientFor(msg.note)
		if c == nil || ed.Buffer().Seq() != st.wantSeq {
			return // the text moved on; the next render asks again
		}
		if text := ed.Buffer().Text(); m.lspSent[tab.vp] != text {
			if _, opened := m.lspSent[tab.vp]; opened {
				_ = c.DidChange(tab.path, text)
				m.lspSent[tab.vp] = text
			}
		}
		gen, seq, lines, path, ch := st.gen, st.wantSeq, ed.Buffer().LineCount(), msg.note, m.termMsgs
		go func() {
			syms, err := c.DocumentSymbols(path)
			out := teaMsg{kind: lspMsgOutline, note: path, sigSeq: gen, bufSeq: seq, outline: syms, hoverOK: err == nil, tab: lines}
			if err != nil {
				out.hoverText = err.Error()
			}
			select {
			case ch <- out:
			default:
			}
		}()
	case lspMsgOutline:
		st.want = false
		if !msg.hoverOK {
			if !st.errNoted {
				st.errNoted = true
				ed.SetMessage("lsp: outline: " + msg.hoverText)
			}
			return // keep the last good outline
		}
		st.errNoted = false
		st.have, st.bufSeq, st.lines, st.syms = true, msg.bufSeq, msg.tab, msg.outline
	}
}

// outlineFor is e's cached outline, nil when there is none.
func (m *Model) outlineFor(e *textbuf.Editor) *outlineState {
	if st := m.outline[e.Path()]; st != nil && st.have {
		return st
	}
	return nil
}

// enclosing is the chain of symbols whose extent holds line, outermost
// first. A symbol that does not nest inside the previous one (a sibling
// on the same line) is left out.
func enclosing(syms []lsp.Symbol, line int) []lsp.Symbol {
	var in []lsp.Symbol
	for _, s := range syms {
		if s.RangeLine <= line && line <= s.EndLine {
			in = append(in, s)
		}
	}
	// Outermost first: earlier start, then later end.
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && outer(in[j], in[j-1]); j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	var chain []lsp.Symbol
	for _, s := range in {
		if n := len(chain); n > 0 {
			p := chain[n-1]
			if s.RangeLine < p.RangeLine || s.EndLine > p.EndLine {
				continue
			}
		}
		chain = append(chain, s)
	}
	return chain
}

func outer(a, b lsp.Symbol) bool {
	if a.RangeLine != b.RangeLine {
		return a.RangeLine < b.RangeLine
	}
	return a.EndLine > b.EndLine
}

// crumbSyms is the symbol chain at e's cursor from the cached outline.
func (m *Model) crumbSyms(e *textbuf.Editor) []lsp.Symbol {
	st := m.outlineFor(e)
	if st == nil {
		return nil
	}
	return enclosing(st.syms, e.Buffer().Cursor().Line)
}

// bufferTitle is the panel title of buffer e (vp: its member/path label,
// panelW the panel's width, chips the markers after the mode pill): the
// name — or, with breadcrumbs, the path and the symbols around the cursor
// — then the dirty dot and the mode.
func (m *Model) bufferTitle(e *textbuf.Editor, vp string, panelW int, chips string) string {
	name := e.Path()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	dot := ""
	if e.Buffer().Dirty() {
		dot = " " + theme.WarningText().Render("●")
	}
	pill := modePill(e.Mode(), e.VisualBlock())
	if n := e.CursorCount(); n > 1 {
		pill += " " + kit.Pill(itoa(n)+" CURSORS", theme.Current.Accent) // F-174
	}
	tail := dot + "  " + pill
	if m.breadcrumbsEnabled(m.editorPrefs()) {
		name = crumbs(pathSegs(vp, name), m.crumbSyms(e), panelW-7-ansi.Width(tail+chips))
	}
	return name + tail
}

// pathSegs splits a member/dir/file label; a buffer with none shows its name.
func pathSegs(vp, name string) []string {
	var out []string
	for _, s := range strings.Split(vp, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{name}
	}
	return out
}

// crumbs joins path › symbols into at most budget cells, dropping from the
// left (a leading "…" says so). The last segment is the one that stays.
func crumbs(path []string, syms []lsp.Symbol, budget int) string {
	segs := append([]string(nil), path...)
	for _, s := range syms {
		segs = append(segs, s.Name)
	}
	sep := " " + theme.GlyphChevron + " "
	sepW := ansi.Width(sep)
	width := func(from int) int {
		w := 0
		for _, s := range segs[from:] {
			w += ansi.Width(s)
		}
		w += sepW * (len(segs) - from - 1)
		if from > 0 {
			w += 1 + sepW // the leading "… › "
		}
		return w
	}
	from, last := 0, len(segs)-1
	for from < last && width(from) > budget {
		from++
	}
	var b strings.Builder
	if from > 0 {
		b.WriteString(theme.TextDim().Render("…" + sep))
	}
	for i := from; i <= last; i++ {
		if i > from {
			b.WriteString(theme.TextDim().Render(sep))
		}
		s := segs[i]
		if avail := budget - boolInt(from > 0)*(1+sepW); i == last && from == last && avail >= 4 && ansi.Width(s) > avail { // even the last one is too long
			s = ansi.Slice(s, 0, avail-1) + "…"
		}
		if i < last {
			s = theme.TextDim().Render(s)
		}
		b.WriteString(s)
	}
	return b.String()
}

// stickyHeaders is the buffer lines to pin above the viewport: the header
// lines of the symbols around the cursor that are scrolled off the top
// (first is the line shown at the top), outermost first, at most stickyMax
// and never so many that they cover the cursor's row (curRow, counted
// from the top). The innermost win when there are more.
func (m *Model) stickyHeaders(e *textbuf.Editor, first, curRow int) []int {
	st := m.outlineFor(e)
	b := e.Buffer()
	if st == nil || (st.bufSeq != b.Seq() && st.lines != b.LineCount()) {
		return nil // no outline, or one an edit has moved
	}
	var lines []int
	seen := map[int]bool{}
	for _, s := range enclosing(st.syms, b.Cursor().Line) {
		if s.Line < first && s.Line < b.LineCount() && !seen[s.Line] {
			seen[s.Line] = true
			lines = append(lines, s.Line)
		}
	}
	if n := min(stickyMax, curRow); len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// stickyRow draws buffer line l as a pinned row: its number, then the
// text on the sticky shade, textW cells wide.
func stickyRow(b *textbuf.Buffer, l, gutW, textW int, numbers bool) string {
	num := " "
	if numbers {
		num = itoa(l + 1)
	}
	text := ansi.Clip(expandTabs(b.Line(l)), textW)
	text += strings.Repeat(" ", max(textW-ansi.Width(text), 0))
	return theme.Hint().Render(padLeft(num, gutW-len(num))) + " " + theme.StickyRow().Render(text)
}
