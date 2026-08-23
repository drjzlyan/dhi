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
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/preview"
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

// WithTermEnv sets the environment for terminal drawer sessions
// (toolchain.Manager.Env output).
func WithTermEnv(env []string) Option {
	return func(m *Model) { m.termEnv = env }
}

// WithLSP enables language-server integration (nil disables).
func WithLSP(mgr *lsp.Manager) Option {
	return func(m *Model) { m.lspMgr = mgr }
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

	bufs      []*bufTab
	activeTab int
	bufFocus  bool

	drawerOpen  bool
	termFocus   bool
	terms       []*termTab
	activeTerm  int
	cancelTerms []context.CancelFunc
	termMsgs    chan teaMsg
	termEnv     []string

	previewOn  bool
	previewKey string // content hash of last rendered preview
	previewDoc string

	gitOpen      bool
	gitFocus     bool
	gitTab       int // 0 status, 1 log
	gitCursor    int
	gitRepo      *gitcore.Repo
	gitEntries   []gitcore.FileStatus
	gitLog       []gitcore.CommitEntry
	gitErr       string
	gitInput     []rune
	gitInputMode bool
	gitMessage   string

	searcher      search.Searcher
	searchQuery   []rune
	hits          []hitRow
	hitList       kit.List
	hitsCh        <-chan search.Hit
	searchCancel  context.CancelFunc
	searching     bool
	searchErr     string
	lastQueryText string

	lspMgr    *lsp.Manager
	lspSent   map[string]string // vpath → last pushed text
	lspDiags  map[string][]lsp.Diagnostic
	compOpen  bool
	compItems []lsp.CompletionItem
	compCur   int
}

type hitRow struct {
	hit  search.Hit
	vp   string // vpath label for display
	text string // line content
}

var _ surfaces.Surface = (*Model)(nil)

// New builds the editor for a workspace; nil ws renders the empty state.
func New(version string, ws *workspace.Workspace, opts ...Option) *Model {
	m := &Model{
		version:  version,
		ws:       ws,
		termMsgs: make(chan teaMsg, 128),
		lspSent:  map[string]string{},
		lspDiags: map[string][]lsp.Diagnostic{},
	}
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

// Init starts the terminal message pump.
func (m *Model) Init() tea.Cmd {
	return m.listenTerm()
}

func (m *Model) listenTerm() tea.Cmd {
	ch := m.termMsgs
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// drainTerm processes any pending drawer messages synchronously.
// Production drains via listenTerm cmds; this keeps headless tests
// deterministic without a running program loop.
func (m *Model) drainTerm() {
	for {
		select {
		case msg := <-m.termMsgs:
			m.Update(msg)
		default:
			return
		}
	}
}

// teaMsg is the union of async drawer/LSP events.
type teaMsg struct {
	kind      uint8 // termMsgOut | termMsgClosed | lspMsgDiag | lspMsgComp
	tab       int
	chunk     []byte
	diags     []lsp.Diagnostic
	compItems []lsp.CompletionItem
}

const (
	termMsgOut uint8 = iota
	termMsgClosed
	lspMsgDiag
	lspMsgComp
)

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
	m.list.Width = railWidth - 4
	m.list.Height = h - 3
	m.findList.Width = 60 - 4
	m.findList.Height = min(12, h-6)
	m.hitList.Width = maxInt(w-railWidth-7, 10)
	m.hitList.Height = h - 5

	if t := m.activeTermTab(); t != nil && t.sess != nil && !t.exited {
		rows := min(drawerHeight, maxInt(h/3, 4)) - 2
		_ = t.sess.Resize(maxInt(w-railWidth-4, 20), rows)
	}
}

// Messages.

type hitMsg search.Hit

type searchDoneMsg struct{}

// Update handles streaming search and terminal messages routed by the shell.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case hitMsg:
		m.addHit(search.Hit(msg))
		return m.listenHits()
	case searchDoneMsg:
		m.searching = false
		m.hitsCh = nil
		return nil
	case teaMsg:
		switch msg.kind {
		case termMsgOut:
			m.ingestTermChunk(msg.tab, msg.chunk)
		case termMsgClosed:
			m.termExited(msg.tab)
		case lspMsgDiag, lspMsgComp:
			m.applyLSPUpdate(msg)
		}
		return m.listenTerm()
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

	if key == "ctrl+t" {
		m.ToggleDrawer()
		return true
	}
	if key == "ctrl+j" {
		m.ToggleGitPanel()
		return true
	}
	if m.drawerOpen && m.termFocus {
		return m.handleTermKey(key)
	}
	if m.gitOpen && m.gitFocus {
		return m.handleGitKey(key)
	}

	// preview toggle works whenever a buffer is open
	if key == "ctrl+g" && m.active() != nil {
		e := m.active()
		if !preview.IsMarkdown(e.Path()) {
			e.SetMessage("preview: not a markdown file")
			return true
		}
		m.previewOn = !m.previewOn
		m.previewKey = "" // force re-render on next View
		return true
	}

	switch m.mode {
	case modeFind:
		return m.handleFindKey(key)
	case modeSearchQuery:
		return m.handleSearchKey(key)
	case modeResults:
		return m.handleResultsKey(key)
	}

	if m.bufFocus && m.active() != nil {
		e := m.active()

		// completion popup intercepts navigation/accept keys
		if m.compOpen {
			if handled := m.handleCompletionKey(key); handled || key == "esc" {
				return true
			}
		}

		// esc in normal mode hands focus back to the tree
		if key == "esc" && e.Mode() == textbuf.ModeNormal {
			m.bufFocus = false
			return true
		}

		// explicit completion request (ctrl+space arrives as either form)
		if e.Mode() == textbuf.ModeInsert && (key == "ctrl+space" || key == "ctrl+@") {
			m.requestCompletion()
			return true
		}

		e.Key(key)
		if e.CloseRequested() && e.TakeClose() {
			m.closeBuffer()
		}
		m.lspSync()
		return true
	}
	return m.handleNavKey(key)
}

// handleTermKey routes keys to the focused terminal session.
func (m *Model) handleTermKey(key string) bool {
	if idx := altDigitIndex(key, len(m.terms)); idx >= 0 {
		m.activeTerm = idx
		return true
	}
	switch key {
	case "alt+n":
		dir, label := m.activeTermDir()
		if dir != "" {
			m.newTermTab(dir, label+"-"+itoa(len(m.terms)+1))
			m.activeTerm = len(m.terms) - 1
		}
		return true
	}
	t := m.activeTermTab()
	if t == nil || t.sess == nil || t.exited {
		return true
	}
	if data, ok := termKeyBytes(key); ok {
		_ = t.sess.Write(data)
	}
	return true
}

func (m *Model) activeTermTab() *termTab {
	if m.activeTerm < len(m.terms) {
		return m.terms[m.activeTerm]
	}
	return nil
}

func (m *Model) activeTermDir() (string, string) {
	if t := m.activeTermTab(); t != nil {
		return t.dir, t.sess.Label()
	}
	if len(m.members) > 0 {
		return m.members[0].path, m.members[0].name
	}
	return "", ""
}

// altDigitIndex maps "alt+1".."alt+9" to a tab index.
func altDigitIndex(key string, count int) int {
	if !strings.HasPrefix(key, "alt+") || len(key) != 5 {
		return -1
	}
	n := int(key[4] - '1')
	if n < 0 || n >= count {
		return -1
	}
	return n
}

// termKeyBytes converts keystroke strings into pty input bytes.
func termKeyBytes(key string) ([]byte, bool) {
	switch key {
	case "enter":
		return []byte{'\r'}, true
	case "backspace":
		return []byte{0x7f}, true
	case "tab":
		return []byte{'\t'}, true
	case "esc":
		return []byte{0x1b}, true
	case "up":
		return []byte("\x1b[A"), true
	case "down":
		return []byte("\x1b[B"), true
	case "right":
		return []byte("\x1b[C"), true
	case "left":
		return []byte("\x1b[D"), true
	case "ctrl+c":
		return []byte{0x03}, true
	case "ctrl+d":
		return []byte{0x04}, true
	case "ctrl+l":
		return []byte{0x0c}, true
	case "ctrl+u":
		return []byte{0x15}, true
	case "space":
		return []byte{' '}, true
	}
	if r := []rune(key); len(r) == 1 && r[0] >= 32 {
		return []byte(key), true
	}
	return nil, false
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

// bufTab is one open buffer with its display identity.
type bufTab struct {
	ed   *textbuf.Editor
	vp   string
	path string
}

// active returns the focused tab's editor, or nil.
func (m *Model) active() *textbuf.Editor {
	if m.activeTab < len(m.bufs) {
		return m.bufs[m.activeTab].ed
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

	for i, t := range m.bufs {
		if t.path == n.path {
			m.activeTab = i // reuse existing buffer
			m.bufFocus = true
			return
		}
	}
	be, err := textbuf.OpenFile(n.path)
	if err != nil {
		m.searchErr = err.Error()
		m.bufFocus = false
		return
	}
	be.SetCommandDelegate(m)
	tab := &bufTab{ed: be, vp: m.openVPath, path: n.path}
	m.bufs = append(m.bufs, tab)
	m.activeTab = len(m.bufs) - 1
	m.bufFocus = true
	m.lspOpenDoc(n.path, be.Buffer().Text())
}

// closeBuffer drops the active tab; focus lands on the neighbor or the
// tree when none remain.
func (m *Model) closeBuffer() {
	if m.activeTab >= len(m.bufs) {
		m.bufFocus = false
		return
	}
	m.bufs = append(m.bufs[:m.activeTab], m.bufs[m.activeTab+1:]...)
	switch {
	case len(m.bufs) == 0:
		m.bufFocus = false
	case m.activeTab >= len(m.bufs):
		m.activeTab = len(m.bufs) - 1
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
	bodyH := m.height
	if m.drawerOpen {
		bodyH = m.height - drawerHeight - 1 // hint line below drawer
		if bodyH < 4 {
			bodyH = 4
		}
	}
	if m.gitOpen {
		bodyH = bodyH - min(gitPanelHeight, maxInt(m.height/3, 5))
		if bodyH < 4 {
			bodyH = 4
		}
	}
	rail := kit.NewPanel("files", false)
	hint := theme.Hint().Render("/ find · s search · ⏎ open · ^t term")
	savedListH := m.list.Height
	m.list.Height = bodyH - 3 // reserve hint row + panel padding
	rail.SetContent(append(splitLines(m.list.View()), "", hint)...)
	rail.Width = railWidth
	rail.Height = bodyH
	m.list.Height = savedListH

	var main string
	title := mainTitle(m.openVPath)
	switch {
	case m.active() != nil && m.previewOn && preview.IsMarkdown(m.active().Path()):
		main = m.previewView()
		title = "preview — " + title
	case m.active() != nil:
		e := m.active()
		title = bufferTitle(e) + m.diagChip(e)
		main = m.bufferView()
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
	centered := kit.Center(main, maxInt(mainW-2, 10), maxInt(bodyH-2, 3))
	if m.mode == modeResults || m.active() != nil {
		centered = main // lists and buffers are left-aligned
	}
	if m.active() != nil {
		centered = joinV(tabStrip(m.bufs, m.activeTab), centered)
	}
	mainPanel := kit.NewPanel(title, true)
	mainPanel.SetContent(splitLines(centered)...)
	mainPanel.Width = mainW
	mainPanel.Height = bodyH

	out := joinH(rail.View(), mainPanel.View())
	if m.gitOpen {
		out += "\n" + m.gitPanelView()
	}
	if m.drawerOpen {
		out += "\n" + m.drawerView()
	}
	return out
}

// ExecEx implements textbuf.CommandDelegate: buffer-list ex commands.
func (m *Model) ExecEx(requester *textbuf.Editor, cmd string) bool {
	switch {
	case cmd == "bn" && len(m.bufs) > 0:
		m.activeTab = (m.activeTab + 1) % len(m.bufs)
		requester.SetMessage("")
		return true
	case cmd == "bp" && len(m.bufs) > 0:
		m.activeTab = (m.activeTab - 1 + len(m.bufs)) % len(m.bufs)
		requester.SetMessage("")
		return true
	case strings.HasPrefix(cmd, "b "):
		pat := strings.TrimSpace(strings.TrimPrefix(cmd, "b "))
		var hits []int
		for i, t := range m.bufs {
			if strings.Contains(t.vp, pat) || strings.Contains(t.path, pat) {
				hits = append(hits, i)
			}
		}
		switch len(hits) {
		case 0:
			requester.SetMessage("no matching buffer: " + pat)
		case 1:
			m.activeTab = hits[0]
			requester.SetMessage("")
		default:
			requester.SetMessage("more than one match for " + pat)
		}
		return true
	}
	return false
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
