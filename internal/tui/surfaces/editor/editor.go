// Package editor is DHI's IDE surface (F-002): multi-repo file navigation,
// modal buffers, terminal drawer, git view, chat sidebar, preview. This
// chunk ships component 1 — workspace nav tree grouped by member repo,
// fuzzy find, and cross-repo ripgrep search; buffers/terminal/git/chat
// land in later M2 chunks.
package editor

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/fuzzy"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/textbuf"
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
	modeSearchQuery
	modeResults
)

// Option configures optional editor capabilities.
type Option func(*Model)

// WithSearcher enables cross-repo ripgrep search. Without it the `s`
// key is inert (ADR-0005: no silent host-tool fallback).
func WithSearcher(s search.Searcher) Option {
	return func(m *Model) { m.searcher = s }
}

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

	buf      *textbuf.Editor
	bufFocus bool

	searcher      search.Searcher
	searchQuery   []rune
	hits          []hitRow
	hitList       kit.List
	hitsCh        <-chan search.Hit
	searchCancel  context.CancelFunc
	searching     bool
	searchErr     string
	lastQueryText string
}

type hitRow struct {
	hit  search.Hit
	vp   string // vpath label for display
	text string // line content
}

var _ surfaces.Surface = (*Model)(nil)

// New builds the editor for a workspace; nil ws renders the empty state.
func New(version string, ws *workspace.Workspace, opts ...Option) *Model {
	m := &Model{version: version, ws: ws}
	if ws != nil {
		for _, mem := range ws.Members {
			m.members = append(m.members, memberRef{name: mem.Name, path: mem.Path})
		}
		m.roots = buildRoots(m.members)
		m.refreshRows()
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "editor", Title: "Editor"} }
func (m *Model) Init() tea.Cmd       { return nil }

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
	m.list.Width = railWidth - 4
	m.list.Height = h - 3
	m.findList.Width = 60 - 4
	m.findList.Height = min(12, h-6)
	m.hitList.Width = maxInt(m.width-railWidth-7, 10)
	m.hitList.Height = h - 5
}

// Messages.

type hitMsg search.Hit

type searchDoneMsg struct{}

// Update handles streaming search results routed by the shell.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case hitMsg:
		m.addHit(search.Hit(msg))
		return m.listenHits()
	case searchDoneMsg:
		m.searching = false
		m.hitsCh = nil
		return nil
	}
	return nil
}

func (m *Model) addHit(h search.Hit) {
	label := filepath.Base(h.Path)
	if vp, err := m.ws.VPathFor(h.Path); err == nil {
		label = vp.String()
	}
	m.hits = append(m.hits, hitRow{hit: h, vp: label, text: strings.TrimSpace(h.Text)})
	path := theme.Hint().Render(label + ":" + itoa(h.Line))
	m.hitList.Items = append(m.hitList.Items, kit.Item{
		Title: path + "  " + theme.TextDim().Render(truncateRunes(strings.TrimSpace(h.Text), 80)),
	})
}

func (m *Model) listenHits() tea.Cmd {
	ch := m.hitsCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		h, ok := <-ch
		if !ok {
			return searchDoneMsg{}
		}
		return hitMsg(h)
	}
}

// HandleKey implements surface key routing.
func (m *Model) HandleKey(key string) bool {
	if m.ws == nil {
		return false
	}
	switch m.mode {
	case modeFind:
		return m.handleFindKey(key)
	case modeSearchQuery:
		return m.handleSearchKey(key)
	case modeResults:
		return m.handleResultsKey(key)
	}

	if m.bufFocus && m.buf != nil {
		// esc in normal mode hands focus back to the tree
		if key == "esc" && m.buf.Mode() == textbuf.ModeNormal {
			m.bufFocus = false
			return true
		}
		m.buf.Key(key)
		if m.buf.CloseRequested() && m.buf.TakeClose() {
			m.closeBuffer()
		}
		return true
	}
	return m.handleNavKey(key)
}

func (m *Model) handleNavKey(key string) bool {
	switch key {
	case "/":
		m.openFind()
		return true
	case "s":
		if m.searcher != nil && len(m.members) > 0 {
			m.openSearch()
			return true
		}
		return false
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
	if be, err := textbuf.OpenFile(n.path); err == nil {
		m.buf = be
		m.bufFocus = true
	} else {
		m.buf = nil
		m.bufFocus = false
		m.searchErr = err.Error()
	}
}

// closeBuffer drops the active buffer and returns focus to the tree.
func (m *Model) closeBuffer() {
	m.buf = nil
	m.bufFocus = false
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
	switch m.mode {
	case modeFind:
		return m.findView()
	case modeSearchQuery:
		return m.searchView()
	default:
		return m.navView()
	}
}

func (m *Model) navView() string {
	rail := kit.NewPanel("files", false)
	hint := theme.Hint().Render("/ find · s search · ⏎ open")
	m.list.Height = m.height - 4 // reserve hint row + panel padding
	rail.SetContent(append(splitLines(m.list.View()), "", hint)...)
	rail.Width = railWidth
	rail.Height = m.height

	var main string
	title := mainTitle(m.openVPath)
	switch {
	case m.buf != nil:
		main = m.bufferView()
		title = bufferTitle(m.buf)
	case m.mode == modeResults:
		main = m.resultsBlock()
		title = "results"
	case m.openPath != "":
		main = strings.Join([]string{
			theme.TabActive().Render(m.openVPath),
			"",
			theme.Hint().Render("press enter on a file to edit"),
		}, "\n")
	default:
		main = theme.TextDim().Render("(select a file to begin editing)")
	}

	mainW := maxInt(m.width-railWidth-1, 10)
	centered := kit.Center(main, maxInt(mainW-2, 10), maxInt(m.height-2, 3))
	if m.mode == modeResults || m.buf != nil {
		centered = main // lists and buffers are left-aligned
	}
	mainPanel := kit.NewPanel(title, true)
	mainPanel.SetContent(splitLines(centered)...)
	mainPanel.Width = mainW
	mainPanel.Height = m.height

	return joinH(rail.View(), mainPanel.View())
}

// Finder (file names).

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

// Content search (ripgrep fan-out across members).

func (m *Model) openSearch() {
	m.searchQuery = nil
	m.searchErr = ""
	m.mode = modeSearchQuery
}

func (m *Model) handleSearchKey(key string) bool {
	switch key {
	case "esc":
		m.mode = modeNav
		return true
	case "enter":
		if q := strings.TrimSpace(string(m.searchQuery)); q != "" {
			m.startSearch(q)
			return true
		}
		return true
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}
		return true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		m.searchQuery = append(m.searchQuery, r[0])
		return true
	}
	return false
}

func (m *Model) startSearch(q string) {
	m.cancelSearch()
	ctx, cancel := context.WithCancel(context.Background())
	m.searchCancel = cancel
	m.hits = nil
	m.hitList = kit.List{Width: m.hitList.Width, Height: m.hitList.Height}
	m.lastQueryText = q
	m.mode = modeResults

	roots := make([]string, len(m.members))
	for i, mem := range m.members {
		roots[i] = mem.path
	}
	ch, err := m.searcher.Search(ctx, q, roots)
	if err != nil {
		m.searching = false
		m.hitsCh = nil
		m.searchErr = err.Error()
		return
	}
	m.searchErr = ""
	m.searching = true
	m.hitsCh = ch
}

func (m *Model) cancelSearch() {
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	m.hitsCh = nil
	m.searching = false
}

func (m *Model) handleResultsKey(key string) bool {
	switch key {
	case "esc":
		m.cancelSearch()
		m.mode = modeNav
		return true
	case "enter", "l":
		return m.jumpHit()
	}
	return m.hitList.HandleKey(key)
}

func (m *Model) jumpHit() bool {
	if m.hitList.Cursor >= len(m.hits) {
		return true
	}
	row := m.hits[m.hitList.Cursor]
	vp, err := workspace.ParseVPath(row.vp)
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
	return true
}

func (m *Model) resultsBlock() string {
	head := theme.TabActive().Render("results for " + strconv.Quote(m.lastQueryText))
	switch {
	case m.searchErr != "":
		return head + "\n\n" + theme.DangerText().Render(m.searchErr)
	case len(m.hits) == 0 && m.searching:
		return head + "\n\n" + theme.TextDim().Render("searching…")
	case len(m.hits) == 0:
		return head + "\n\n" + theme.TextDim().Render("no matches")
	}
	count := theme.Hint().Render(itoa(len(m.hits)) + " hit(s)" + searchStateSuffix(m.searching))
	lines := append([]string{head + "  " + count, ""}, splitLines(m.hitList.View())...)
	lines = append(lines, "", theme.Hint().Render("⏎ jump · esc back"))
	return strings.Join(lines, "\n")
}

func searchStateSuffix(searching bool) string {
	if searching {
		return " · searching…"
	}
	return ""
}

func (m *Model) searchView() string {
	head := "/ " + string(m.searchQuery) + "▌"
	body := []string{
		theme.Brand().Render(head),
		"",
		theme.TextDim().Render("fixed-string content search across all member repos"),
	}
	overlay := kit.NewPanel("search", true)
	overlay.SetContent(body...)
	overlay.Width = 64
	overlay.Height = min(8, m.height)

	dim := theme.Hint().Render("enter search · esc cancel")
	return kit.Center(joinV(overlay.View(), "", dim), m.width, m.height)
}

// Small local helpers.

func itoa(n int) string { return strconv.Itoa(n) }

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
