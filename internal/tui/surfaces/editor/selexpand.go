package editor

import (
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
)

// Selection expand / shrink (F-167): alt+o grows the selection, alt+i walks
// back. With a language server that advertises selectionRange the next
// range is its syntax tree's; otherwise (or when it has nothing larger, or
// fails) the structural ladder in textbuf decides, and the status line
// names what was used so the fallback is never silent.

// selectionKey handles alt+o / alt+i in normal and visual mode.
func (m *Model) selectionKey(key string) {
	e := m.active()
	if e == nil {
		return
	}
	if key == "alt+i" {
		if !e.ShrinkSelection() {
			e.SetMessage("shrink: nothing to shrink back to")
		}
		return
	}
	a, z, ok := e.SelectionBounds()
	if !ok {
		e.SetMessage("expand: a block selection cannot expand (esc, then v)")
		return
	}
	c := m.lspClient()
	switch {
	case c == nil:
		m.structuralExpand("no language server")
	case !c.CanSelectionRange():
		m.structuralExpand("server has no selectionRange")
	default:
		m.requestSelectionRange(c, a, z)
	}
}

// structuralExpand takes the next structural rung and says why it did.
func (m *Model) structuralExpand(why string) {
	e := m.active()
	if name := e.StructuralExpand(); name != "" {
		e.SetMessage("expand: " + name + " (structural: " + why + ")")
	} else {
		e.SetMessage("expand: nothing larger (" + why + ")")
	}
}

// requestSelectionRange asks the server for the ranges around the
// selection's start; the answer is applied by applySelectionRange.
func (m *Model) requestSelectionRange(c *lsp.Client, a, z textbuf.Pos) {
	e := m.active()
	path := e.Path()
	line, col := a.Line, u16(e, a.Line, a.Col)
	m.lspSync() // the server must see the text the positions refer to
	go func() {
		rs, err := c.SelectionRanges(path, line, col)
		msg := teaMsg{kind: lspMsgSel, selRanges: rs, selFrom: a, selTo: z, diagPath: path}
		if err != nil {
			msg.note = err.Error()
		}
		select {
		case m.termMsgs <- msg:
		default:
		}
	}()
}

// applySelectionRange picks the smallest server range that strictly
// contains the selection the request was made for, else falls back.
func (m *Model) applySelectionRange(msg teaMsg) {
	e := m.active()
	if e == nil || e.Path() != msg.diagPath {
		return
	}
	if a, z, ok := e.SelectionBounds(); !ok || a != msg.selFrom || z != msg.selTo {
		return // the user moved on while the server thought
	}
	buf := e.Buffer()
	pos := func(p lsp.Position) textbuf.Pos {
		return textbuf.Pos{Line: p.Line, Col: lsp.RuneCol(buf.Line(p.Line), p.Character)}
	}
	if msg.note != "" {
		m.structuralExpand("selectionRange failed: " + msg.note)
		return
	}
	for _, r := range msg.selRanges { // innermost first
		if r.Start.Line >= buf.LineCount() || r.End.Line >= buf.LineCount() {
			continue
		}
		a, z := pos(r.Start), pos(r.End)
		if z.Line < a.Line || z.Line == a.Line && z.Col <= a.Col {
			continue
		}
		if z.Col == 0 && z.Line > a.Line {
			z = textbuf.Pos{Line: z.Line - 1, Col: len([]rune(buf.Line(z.Line - 1)))} // a selection cannot end on a line break
		}
		contains := !msg.selFrom.Before(a) && !z.Before(msg.selTo)
		if contains && (a != msg.selFrom || z != msg.selTo) {
			e.ApplySelection(a, z)
			e.SetMessage("expand: language server")
			return
		}
	}
	m.structuralExpand("server has no larger range")
}
