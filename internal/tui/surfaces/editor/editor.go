// Package editor is DHI's IDE surface (F-002): multi-repo file navigation,
// modal buffers, terminal drawer, git view, chat sidebar, preview. This
// chunk ships component 1 — the workspace nav tree grouped by member repo
// with fuzzy find; buffers/terminal/git/chat land in later M2 chunks.
package editor

import (
	"path/filepath"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/fuzzy"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const (
	railWidth   = 34
	findCapRows = 200 // finder result rows rendered
	indexCap    = 20000
)

type mode uint8

const (
	modeNav mode = iota
	modeFind
)

// Model is the Editor surface.
type Model struct {
	version string
	ws      *workspace.Workspace
	members []memberRef
	roots   []*node
	rows    []treeRow
	list    kit.List
	width   int
	height  int

	mode      mode
	query     []rune
	results   []fuzzy.Result
	items     []string // indexed vpaths for fuzzy find
	findList  kit.List
	openPath  string // absolute path of opened file
	openVPath string
}

var _ surfaces.Surface = (*Model)(nil)

// New builds the editor for a workspace; nil ws renders the empty state.
func New(version string, ws *workspace.Workspace) *Model {
	m := &Model{version: version, ws: ws}
	if ws != nil {
		for _, mem := range ws.Members {
			m.members = append(m.members, memberRef{name: mem.Name, path: mem.Path})
		}
		m.roots = buildRoots(m.members)
		m.refreshRows()
	}
	m.list.Height = 0 // set on Resize
	return m
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "editor", Title: "Editor"} }
func (m *Model) Init() tea.Cmd       { return nil }

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
	m.list.Width = railWidth - 4
	m.list.Height = h - 2
	m.findList.Width = 60 - 4
	m.findList.Height = min(12, h-6)
}

func (m *Model) Update(tea.Msg) tea.Cmd { return nil }

// HandleKey implements surface key routing.
func (m *Model) HandleKey(key string) bool {
	if m.ws == nil {
		return false
	}
	switch m.mode {
	case modeFind:
		return m.handleFindKey(key)
	default:
		return m.handleNavKey(key)
	}
}

func (m *Model) handleNavKey(key string) bool {
	switch key {
	case "/":
		m.openFind()
		return true
	case "enter", "l":
		n := m.cursorNode()
		if n == nil {
			return true
		}
		switch n.kind {
		case nodeFile:
			m.open(n)
		default:
			n.toggle()
			m.refreshRows()
		}
		return true
	case "h":
		if n := m.cursorNode(); n != nil && n.kind != nodeFile && n.expanded {
			n.toggle()
			m.refreshRows()
		}
		return true
	}
	return m.list.HandleKey(key)
}

func (m *Model) cursorNode() *node {
	if m.list.Cursor < len(m.rows) {
		return m.rows[m.list.Cursor].node
	}
	return nil
}

func (m *Model) open(n *node) {
	m.openPath = n.path
	vp, err := m.ws.VPathFor(n.path)
	if err == nil {
		m.openVPath = vp.String()
	} else {
		m.openVPath = n.name
	}
}

// refreshRows re-flattens the tree into list items.
func (m *Model) refreshRows() {
	m.rows = flatten(m.roots)
	items := make([]kit.Item, len(m.rows))
	for i, r := range m.rows {
		it := kit.Item{}
		indent := strings.Repeat("  ", r.depth)
		switch r.node.kind {
		case nodeRepo:
			it.Title = theme.TabActive().Render(r.node.name + "/")
			if !r.node.expanded {
				it.Title += theme.Hint().Render(" ▸")
			}
		case nodeDir:
			it.Title = indent + theme.TextDim().Render(r.node.name+"/")
		case nodeFile:
			it.Title = indent + r.node.name
		}
		items[i] = it
	}
	cur := m.list.Cursor
	m.list.SetItems(items)
	if cur < len(items) {
		m.list.Cursor = cur
	}
}

func (m *Model) View() string {
	if m.ws == nil {
		return kit.Center(
			theme.TextDim().Render("no workspace loaded — open DHI inside a directory with .dhi/workspace.toml"),
			m.width, m.height,
		)
	}
	if m.mode == modeFind {
		return m.findView()
	}
	return m.navView()
}

func (m *Model) navView() string {
	rail := kit.NewPanel("files", false)
	rail.SetContent(splitLines(m.list.View())...)
	rail.Width = railWidth
	rail.Height = m.height

	main := theme.TextDim().Render("(buffers arrive with modal editor — M2)")
	if m.openPath != "" {
		main = strings.Join([]string{
			theme.TabActive().Render(m.openVPath),
			"",
			theme.Hint().Render("modal editing lands in the next M2 chunk"),
		}, "\n")
	}
	mainW := maxInt(m.width-railWidth-1, 10)
	centered := kit.Center(main, maxInt(mainW-2, 10), maxInt(m.height-2, 3))
	mainPanel := kit.NewPanel(mainTitle(m.openVPath), true)
	mainPanel.SetContent(splitLines(centered)...)
	mainPanel.Width = mainW
	mainPanel.Height = m.height

	return joinH(rail.View(), mainPanel.View())
}

// Finder.

func (m *Model) openFind() {
	if m.items == nil {
		m.items = indexFiles(m.roots, indexCap)
	}
	m.query = nil
	m.mode = modeFind
	m.applyQuery()
}

func (m *Model) applyQuery() {
	pat := string(m.query)
	m.results = fuzzy.Rank(pat, m.items)
	limit := len(m.results)
	if limit > findCapRows {
		limit = findCapRows
	}
	items := make([]kit.Item, 0, limit)
	for _, r := range m.results[:limit] {
		items = append(items, kit.Item{Title: m.items[r.Index]})
	}
	m.findList.SetItems(items)
}

func (m *Model) handleFindKey(key string) bool {
	switch key {
	case "esc":
		m.mode = modeNav
		return true
	case "enter":
		return m.pickResult()
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.applyQuery()
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		m.query = append(m.query, r[0])
		m.applyQuery()
		return true
	}
	return m.findList.HandleKey(key)
}

func (m *Model) pickResult() bool {
	sel, ok := m.findList.Selected()
	if !ok || sel.Title == "" {
		return true
	}
	idx := m.findList.Cursor
	if idx >= len(m.results) {
		return true
	}
	vpathStr := m.items[m.results[idx].Index]
	vp, err := workspace.ParseVPath(vpathStr)
	if err != nil {
		return true
	}
	abs, err := m.ws.Resolve(vp)
	if err != nil {
		return true
	}
	revealTo(m.roots, abs)
	m.refreshRows()
	if n := findByPath(m.roots, abs); n != nil {
		if i, ok := rowIndex(m.rows, n); ok {
			m.list.Cursor = clampIdx(i, len(m.rows)-1)
		}
	}
	m.open(&node{kind: nodeFile, path: abs, name: filepath.Base(abs)})
	m.mode = modeNav
	return true
}

func (m *Model) findView() string {
	head := "> " + string(m.query) + "▌"
	var body []string
	body = append(body, theme.Brand().Render(head), "")
	if len(m.findList.Items) == 0 {
		body = append(body, theme.TextDim().Render("  no matches"))
	}
	body = append(body, splitLines(m.findList.View())...)
	overlay := kit.NewPanel("find file", true)
	overlay.SetContent(body...)
	overlay.Width = 60
	overlay.Height = min(16, m.height)

	dim := theme.Hint().Render("enter open · esc cancel · type to filter")
	return kit.Center(joinV(overlay.View(), "", dim), m.width, m.height)
}
