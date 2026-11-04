package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Call hierarchy (F-173): `:calls` lists who calls the function under the
// cursor, `:calls out` what it calls. The answer is a tree: right/l opens
// a node one level deeper (its own callers or callees, asked of the server
// then), left/h closes it, enter jumps to the function.

type callNode struct {
	item    lsp.CallItem
	depth   int
	open    bool
	loaded  bool
	loading bool
	err     string
	kids    []*callNode
}

type callPicker struct {
	out  bool // outgoing calls
	root *callNode
	cur  int
	c    *lsp.Client
}

// callsMsg is the server's answer for one node's children.
type callsMsg struct {
	p    *callPicker
	node *callNode
	kids []lsp.CallItem
	err  error
}

// callsCommand handles :calls, :calls in and :calls out. It returns the
// status message and whether cmd was the calls command; on success the
// picker opens (message "").
func (m *Model) callsCommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "calls" || len(f) > 2 {
		return "", false
	}
	out := false
	if len(f) == 2 {
		switch f[1] {
		case "in":
		case "out":
			out = true
		default:
			return "calls: use :calls (incoming) or :calls out", true
		}
	}
	e := m.active()
	if e == nil {
		return "calls: open a file first", true
	}
	c := m.clientFor(e.Path())
	if c == nil {
		return "calls: no language server for this file", true
	}
	if !c.CanCallHierarchy() {
		return "calls: this language server does not offer call hierarchy", true
	}
	cur := e.Buffer().Cursor()
	word, start := wordAt(e.Buffer().Line(cur.Line), cur.Col)
	if word == "" {
		return "calls: no identifier under cursor", true
	}
	path, line, col := e.Path(), cur.Line, u16(e, cur.Line, start)
	items, err := withTimeout(func() ([]lsp.CallItem, error) { return c.PrepareCallHierarchy(path, line, col) })
	if err != nil {
		return "calls failed: " + err.Error(), true
	}
	if len(items) == 0 {
		return "calls: " + word + " is not a function or method", true
	}
	p := &callPicker{out: out, c: c, root: &callNode{item: items[0], open: true}}
	m.calls, m.mode = p, modeCalls
	m.queue(p.load(p.root))
	return "", true
}

// load asks the server for node's calls.
func (p *callPicker) load(n *callNode) tea.Cmd {
	n.loading = true
	c, out, item := p.c, p.out, n.item
	return func() tea.Msg {
		kids, err := withTimeout(func() ([]lsp.CallItem, error) {
			if out {
				return c.OutgoingCalls(item)
			}
			return c.IncomingCalls(item)
		})
		return callsMsg{p: p, node: n, kids: kids, err: err}
	}
}

func (m *Model) onCalls(msg callsMsg) {
	n := msg.node
	n.loading, n.loaded = false, true
	if msg.err != nil {
		n.err = msg.err.Error()
		return
	}
	for _, it := range msg.kids {
		n.kids = append(n.kids, &callNode{item: it, depth: n.depth + 1})
	}
}

// rows flattens the open part of the tree.
func (p *callPicker) rows() []*callNode {
	var out []*callNode
	var walk func(n *callNode)
	walk = func(n *callNode) {
		out = append(out, n)
		if n.open {
			for _, k := range n.kids {
				walk(k)
			}
		}
	}
	walk(p.root)
	return out
}

func (m *Model) handleCallsKey(key string) bool {
	p := m.calls
	if p == nil {
		m.mode = modeNav
		return false
	}
	rows := p.rows()
	if p.cur >= len(rows) {
		p.cur = len(rows) - 1
	}
	n := rows[p.cur]
	switch key {
	case "esc", "q":
		m.mode, m.calls = modeNav, nil
	case "enter":
		it := n.item
		m.mode, m.calls = modeNav, nil
		m.bufFocus = true
		m.gotoLocation(it.PathFor(), it.Select.Start)
	case "down", "j", "ctrl+n":
		if p.cur < len(rows)-1 {
			p.cur++
		}
	case "up", "k", "ctrl+p":
		if p.cur > 0 {
			p.cur--
		}
	case "right", "l", "tab", " ":
		if n.open {
			break
		}
		n.open = true
		if !n.loaded && !n.loading {
			m.queue(p.load(n))
		}
	case "left", "h":
		if n.open && n.depth > 0 {
			n.open = false
			break
		}
		for i := p.cur - 1; i >= 0; i-- { // closed already: step up to the parent
			if rows[i].depth < n.depth {
				p.cur = i
				break
			}
		}
	}
	return true
}

func (m *Model) callsView() string {
	p := m.calls
	dir := "incoming calls"
	if p.out {
		dir = "outgoing calls"
	}
	rows := p.rows()
	body := make([]string, 0, len(rows)+2)
	start := 0
	const window = 14
	if p.cur >= window {
		start = p.cur - window + 1
	}
	for i := start; i < len(rows) && i < start+window; i++ {
		n := rows[i]
		mark := theme.GlyphBullet
		switch {
		case n.open && (len(n.kids) > 0 || n.loading):
			mark = theme.GlyphDirOpen
		case !n.loaded || len(n.kids) > 0:
			mark = theme.GlyphDirClosed
		}
		row := fmt.Sprintf("%s%s %-8s %s", strings.Repeat("  ", n.depth), mark, n.item.Kind, n.item.Name)
		where := m.callWhere(n.item)
		switch {
		case n.loading:
			where += " …"
		case n.err != "":
			where += " (" + n.err + ")"
		case n.loaded && len(n.kids) == 0:
			where += " (none)"
		case n.depth > 0 && len(n.item.Sites) > 1:
			where += fmt.Sprintf(" ×%d", len(n.item.Sites))
		}
		line := row + "  " + theme.TextDim().Render(where)
		if i == p.cur {
			line = theme.Chip().Render(" " + row + "  " + where + " ")
		}
		body = append(body, line)
	}
	box := kit.NewPanel(dir+" of "+p.root.item.Name, true)
	box.SetContent(body...)
	box.Width = 72
	box.Height = min(len(body)+2, m.height)
	hint := theme.Hint().Render("enter jump · →/← open/close a level · ↑/↓ move · esc cancel")
	return kit.Center(joinV(box.View(), "", hint), m.width, m.height)
}

// callWhere is "file:line" for an item, as member/rel when inside one.
func (m *Model) callWhere(it lsp.CallItem) string {
	p := it.PathFor()
	name := filepath.Base(p)
	if vp, ok := m.memberPath(p); ok {
		name = vp
	}
	return fmt.Sprintf("%s:%d", name, it.Select.Start.Line+1)
}
