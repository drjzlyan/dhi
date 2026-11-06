package editor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Document outline / go-to-symbol (F-040): `:sym` lists the symbols the
// language server reports for the active buffer; typing filters, enter
// jumps the cursor to the chosen one.

type symbolPicker struct {
	all   []lsp.Symbol
	query []rune
	cur   int
}

// filtered returns the symbols whose name or kind contains the query
// (case-insensitive), in outline order.
func (p *symbolPicker) filtered() []lsp.Symbol {
	if len(p.query) == 0 {
		return p.all
	}
	q := strings.ToLower(string(p.query))
	var out []lsp.Symbol
	for _, s := range p.all {
		if strings.Contains(strings.ToLower(s.Name), q) || strings.Contains(s.Kind, q) {
			out = append(out, s)
		}
	}
	return out
}

// symCommand handles :sym. It returns the status message and whether cmd
// was the symbol command; on success the picker opens (message "").
func (m *Model) symCommand(cmd string) (string, bool) {
	if strings.TrimSpace(cmd) != "sym" {
		return "", false
	}
	e := m.active()
	if e == nil {
		return "sym: open a file first", true
	}
	c := m.clientFor(e.Path())
	if c == nil {
		return "sym: no language server for this file", true
	}
	// The server may still be indexing (seconds): ask off the UI loop and
	// open the picker when symsMsg lands.
	path := e.Path()
	m.queue(func() tea.Msg {
		syms, err := withTimeout(func() ([]lsp.Symbol, error) { return c.DocumentSymbols(path) })
		return symsMsg{path: path, syms: syms, err: err}
	})
	return "sym: asking the language server…", true
}

// symsMsg is the server's document symbols for :sym.
type symsMsg struct {
	path string
	syms []lsp.Symbol
	err  error
}

// onSyms opens the picker if the file is still the one in front.
func (m *Model) onSyms(msg symsMsg) {
	e := m.active()
	if e == nil || e.Path() != msg.path {
		return
	}
	switch {
	case msg.err != nil:
		e.SetMessage("sym failed: " + msg.err.Error())
	case len(msg.syms) == 0:
		e.SetMessage("sym: the server reports no symbols")
	default:
		e.SetMessage("")
		m.syms = &symbolPicker{all: msg.syms}
		m.mode = modeSymbols
	}
}

func (m *Model) handleSymbolKey(key string) bool {
	p := m.syms
	if p == nil {
		m.mode = modeNav
		return false
	}
	items := p.filtered()
	switch key {
	case "esc":
		m.mode, m.syms = modeNav, nil
	case "enter":
		if p.cur < len(items) {
			m.jumpToSymbol(items[p.cur])
		}
		m.mode, m.syms = modeNav, nil
	case "down", "ctrl+n":
		if p.cur < len(items)-1 {
			p.cur++
		}
	case "up", "ctrl+k":
		if p.cur > 0 {
			p.cur--
		}
	default:
		if q, ok := kit.EditQuery(string(p.query), key); ok {
			p.query, p.cur = []rune(q), 0
		}
	}
	return true
}

func (m *Model) jumpToSymbol(s lsp.Symbol) {
	if e := m.active(); e != nil {
		e.Buffer().SetCursor(textbuf.Pos{Line: s.Line, Col: s.Col})
		m.bufFocus = true
	}
}

func (m *Model) symbolsView() string {
	p := m.syms
	items := p.filtered()
	body := []string{theme.Brand().Render("> " + string(p.query) + "▌"), ""}
	if len(items) == 0 {
		body = append(body, theme.TextDim().Render("  no matching symbols"))
	}
	start := 0
	const window = 14
	if p.cur >= window {
		start = p.cur - window + 1
	}
	for i := start; i < len(items) && i < start+window; i++ {
		s := items[i]
		row := fmt.Sprintf("%s%-9s %s", strings.Repeat("  ", s.Depth), s.Kind, s.Name)
		line := fmt.Sprintf("%s  %s", row, theme.TextDim().Render(fmt.Sprintf(":%d", s.Line+1)))
		if i == p.cur {
			line = theme.Chip().Render(" " + row + " ")
		}
		body = append(body, line)
	}
	box := kit.NewPanel("symbols", true)
	box.SetContent(body...)
	box.Width = 64
	box.Height = min(len(body)+2, m.height)
	hint := theme.Hint().Render("enter jump · ↑/↓ move · type to filter · esc cancel")
	return kit.Center(joinV(box.View(), "", hint), m.width, m.height)
}
