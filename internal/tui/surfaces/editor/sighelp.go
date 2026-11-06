package editor

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Signature help (F-128): typing a server trigger character in insert
// mode ("(" and "," for most servers) pops up the signature of the call
// being typed with the active parameter highlighted.

// signatureKey runs after every key the buffer handled. In insert mode a
// trigger character asks for the signature of the call being typed; while
// the popup is open a backspace or a ")" that leaves the cursor inside a
// call refreshes it (the active parameter moved, or an enclosing call is
// now the one being typed), and one that leaves the call, or any exit from
// insert mode, closes it. "In a call" is judged on the cursor line only.
func (m *Model) signatureKey(e *textbuf.Editor, key string, was textbuf.Mode) {
	if e.Mode() != textbuf.ModeInsert {
		m.dismissSignature()
		return
	}
	if was != textbuf.ModeInsert {
		return
	}
	cur := e.Buffer().Cursor()
	inCall := openParensBefore(e.Buffer().Line(cur.Line), cur.Col) > 0
	if m.sigHelp != nil && (!inCall || m.sigPath != e.Path()) {
		m.dismissSignature()
	}
	if !inCall {
		return
	}
	c := m.lspClient()
	if c == nil {
		return
	}
	trigger := false
	for _, t := range c.SignatureTriggers() {
		if key == t {
			trigger = true
		}
	}
	if !trigger && (m.sigHelp == nil || (key != "backspace" && key != ")")) {
		return
	}
	m.sigSeq++
	seq, path, line := m.sigSeq, e.Path(), cur.Line
	col := u16(e, cur.Line, cur.Col)
	go func() {
		h, err := c.SignatureHelp(path, line, col)
		if err != nil {
			h = nil
		}
		select {
		case m.termMsgs <- teaMsg{kind: lspMsgSig, sig: h, sigSeq: seq, note: path}:
		default:
		}
	}()
}

// openParensBefore counts the "(" still unclosed before rune column col.
func openParensBefore(line string, col int) int {
	r := []rune(line)
	n := 0
	for _, ch := range r[:clampIdx(col, len(r))] {
		switch ch {
		case '(':
			n++
		case ')':
			if n > 0 {
				n--
			}
		}
	}
	return n
}

// applySignature shows an answer unless a newer request, or leaving
// insert mode, overtook it.
func (m *Model) applySignature(msg teaMsg) {
	e := m.active()
	if msg.sigSeq != m.sigSeq || e == nil || e.Mode() != textbuf.ModeInsert || e.Path() != msg.note {
		return
	}
	m.sigHelp, m.sigPath = msg.sig, msg.note
}

func (m *Model) dismissSignature() {
	if m.sigHelp == nil && m.sigPath == "" {
		return
	}
	m.sigHelp, m.sigPath = nil, ""
	m.sigSeq++ // an answer still in flight must not reopen it
}

// signatureView renders the signature with the active parameter in the
// brand colour, and the first line of its doc below.
func (m *Model) signatureView() []string {
	e := m.active()
	if m.sigHelp == nil || e == nil || e.Mode() != textbuf.ModeInsert || e.Path() != m.sigPath {
		return nil
	}
	w := maxInt(m.surfW()-10, 20)
	r := []rune(m.sigHelp.Label)
	s, z := m.sigHelp.ParamStart, m.sigHelp.ParamEnd
	if len(r) > w { // window the label so the active parameter stays in view
		from := clampIdx(z-w, len(r)-w)
		r, s, z = r[from:from+w], s-from, z-from
	}
	s, z = clampIdx(s, len(r)), clampIdx(z, len(r))
	row := theme.TextDim().Render(string(r[:s]))
	if z > s {
		row += theme.Brand().Render(string(r[s:z]))
	}
	row += theme.TextDim().Render(string(r[z:]))
	rows := []string{row}
	if doc, _, _ := strings.Cut(m.sigHelp.Doc, "\n"); doc != "" {
		rows = append(rows, theme.Hint().Render(truncateRunes(doc, w)))
	}
	return rows
}
