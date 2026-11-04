package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Peek (F-173): `gp` shows the definition of the identifier under the
// cursor, `:peek refs` its references, in a popup under the cursor, without
// leaving the buffer. esc closes it, enter jumps there, up/down (j/k) step
// through several locations; any other key closes it and carries on, as
// with hover. (vim's own gp, put-and-move-after, is not implemented here.)

const peekRows = 10

type peekState struct {
	title string
	locs  []lsp.Location
	cur   int
}

type peekMsg struct {
	refs bool
	word string
	locs []lsp.Location
	err  error
}

// peekCommand handles :peek and :peek refs.
func (m *Model) peekCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "peek" || len(f) > 2 {
		return "", false
	}
	refs := false
	if len(f) == 2 {
		if f[1] != "refs" && f[1] != "def" {
			return "peek: use :peek (definition) or :peek refs", true
		}
		refs = f[1] == "refs"
	}
	m.requestPeek(refs)
	return "", true
}

// requestPeek asks the server for the definition (or references) of the
// identifier under the cursor.
func (m *Model) requestPeek(refs bool) {
	c := m.lspClient()
	e := m.active()
	if e == nil {
		return
	}
	if c == nil {
		e.SetMessage("lsp: no language server for this file")
		return
	}
	cur := e.Buffer().Cursor()
	word, start := wordAt(e.Buffer().Line(cur.Line), cur.Col)
	if word == "" {
		e.SetMessage("lsp: no identifier under cursor")
		return
	}
	path, line, col := e.Path(), cur.Line, u16(e, cur.Line, start)
	m.queue(func() tea.Msg {
		var locs []lsp.Location
		var err error
		if refs {
			locs, err = withTimeout(func() ([]lsp.Location, error) { return c.References(path, line, col) })
		} else {
			locs, err = withTimeout(func() ([]lsp.Location, error) { return c.Definition(path, line, col) })
		}
		return peekMsg{refs: refs, word: word, locs: locs, err: err}
	})
}

func (m *Model) onPeek(msg peekMsg) {
	e := m.active()
	what, title := "definition", "definition of "+msg.word
	if msg.refs {
		what, title = "references", "references to "+msg.word
	}
	switch {
	case msg.err != nil:
		if e != nil {
			e.SetMessage("lsp: " + msg.err.Error())
		}
	case len(msg.locs) == 0:
		if e != nil {
			e.SetMessage("lsp: no " + what + " for " + msg.word)
		}
	default:
		m.peek = &peekState{title: title, locs: msg.locs}
	}
}

func (m *Model) handlePeekKey(key string) bool {
	p := m.peek
	switch key {
	case "esc":
		m.peek = nil
	case "enter":
		l := p.locs[p.cur]
		m.peek = nil
		m.gotoLocation(l.PathFor(), l.Range.Start)
	case "down", "j", "ctrl+n":
		p.cur = min(p.cur+1, len(p.locs)-1)
	case "up", "k", "ctrl+p":
		p.cur = max(p.cur-1, 0)
	default:
		m.peek = nil
		if e := m.active(); e != nil {
			e.Key(key) // close-and-process, as hover does
			m.lspSync()
		}
	}
	return true
}

// peekSource returns up to n lines of path from line start: the open
// buffer's text when there is one, else the file's.
func (m *Model) peekSource(path string, start, n int) []string {
	var lines []string
	for _, t := range m.bufs {
		if t.ed != nil && t.path == path {
			b := t.ed.Buffer()
			for i := start; i < b.LineCount() && len(lines) < n; i++ {
				lines = append(lines, b.Line(i))
			}
			return lines
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"(unreadable: " + err.Error() + ")"}
	}
	all := strings.Split(string(data), "\n")
	for i := start; i < len(all) && len(lines) < n; i++ {
		lines = append(lines, strings.TrimSuffix(all[i], "\r"))
	}
	return lines
}

// peekView renders the peek popup rows: a header, then peekRows lines of
// the target with the target line marked.
func (m *Model) peekView() []string {
	p := m.peek
	if p == nil || len(p.locs) == 0 {
		return nil
	}
	l := p.locs[p.cur]
	path := l.PathFor()
	name := filepath.Base(path)
	if vp, ok := m.memberPath(path); ok {
		name = vp
	}
	head := "peek: " + p.title
	if len(p.locs) > 1 {
		head += fmt.Sprintf(" (%d/%d)", p.cur+1, len(p.locs))
	}
	head += fmt.Sprintf(" · %s:%d", name, l.Range.Start.Line+1)
	width := maxInt(m.surfW()-12, 20)
	rows := []string{theme.Hint().Render(truncateRunes(head, width))}
	first := max(l.Range.Start.Line-2, 0)
	for i, ln := range m.peekSource(path, first, peekRows) {
		num := first + i + 1
		text := fmt.Sprintf("%4d %s", num, truncateRunes(expandTabs(ln), width))
		if first+i == l.Range.Start.Line {
			rows = append(rows, theme.Chip().Render(text))
		} else {
			rows = append(rows, theme.TextDim().Render(text))
		}
	}
	return rows
}
