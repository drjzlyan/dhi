package editor

import (
	"fmt"
	"strings"

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
	path := e.Path()
	syms, err := withTimeout(func() ([]lsp.Symbol, error) { return c.DocumentSymbols(path) })
	if err != nil {
		return "sym failed: " + err.Error(), true
	}
	if len(syms) == 0 {
		return "sym: the server reports no symbols", true
	}
	m.syms = &symbolPicker{all: syms}
	m.mode = modeSymbols
	return "", true
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
	case "up", "ctrl+p":
		if p.cur > 0 {
			p.cur--
		}
	case "backspace":
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.cur = 0
		}
	default:
		if r := []rune(key); len(r) == 1 && r[0] >= 32 {
			p.query = append(p.query, r[0])
			p.cur = 0
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
