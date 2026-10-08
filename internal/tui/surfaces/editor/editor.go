// Package editor is DHI's IDE surface (F-002): multi-repo file navigation,
// modal buffers, terminal drawer, git view, chat sidebar, preview. This
// chunk ships component 1 — workspace nav tree grouped by member repo,
// fuzzy find, and cross-repo ripgrep search; buffers/terminal/git/chat
// land in later M2 chunks.
package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/runtime"
	"github.com/drjzlyan/dhi/internal/fuzzy"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/langserver"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/preview"
	"github.com/drjzlyan/dhi/internal/search"
	"github.com/drjzlyan/dhi/internal/testrun"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/tutorial"
	"github.com/drjzlyan/dhi/internal/unread"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const (
	findCapRows = 200 // finder result rows rendered
	indexCap    = 20000
	// agentEditWindow is how long an agent-applied edit keeps its
	// active-editing indicator visible (F-035 Part B).
	agentEditWindow = 6 * time.Second
)

type mode uint8

const (
	modeNav mode = iota
	modeFind
	modeSearchQuery
	modeResults
	modeSymbols
	modeTests
	modeDebug
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

// SetEmitter implements surfaces.Emitter: the shell learns what the user
// just did so a running tutorial can advance on the real action.
func (m *Model) SetEmitter(fn func(event string)) { m.emit = fn }

// act reports one user action (UI goroutine only).
func (m *Model) act(event string) {
	if m.emit != nil {
		m.emit(event)
	}
}

// WithLanguages sets the language table (built-ins plus the user's
// [editor.languages] overrides). Without it the built-ins apply.
func WithLanguages(r langserver.Registry) Option {
	return func(m *Model) { m.langs = r }
}

// LSPInstaller installs npm packages into prefix with DHI's own npm. The
// editor calls it only after the user confirmed the exact packages.
type LSPInstaller func(ctx context.Context, prefix string, packages []string) error

// WithLSPInstaller enables `:lsp install` (nil leaves it explaining why not).
func WithLSPInstaller(fn LSPInstaller) Option {
	return func(m *Model) { m.lspInstaller = fn }
}

// WithChat attaches the agent runtime powering the crew sidebar (F-007).
// nil leaves ctrl+a inert.
func WithChat(rt *runtime.Runtime) Option {
	return func(m *Model) { m.chat = newChat(rt) }
}

// WithUnread shares the F-017 read-mark store with the chat sidebar so
// both surfaces see one read state (rail markers clear in lockstep).
func WithUnread(us *unread.Store) Option {
	return func(m *Model) {
		if m.chat != nil {
			m.chat.unread = us
		}
	}
}

// WithIdentity installs the git identity resolver used by the git
// panel's commit path (F-029). A nil resolver refuses by name.
func WithIdentity(fn gitcore.IdentityFunc) Option {
	return func(m *Model) { m.gitIdentity = fn }
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
	items     *fuzzy.Index // pre-lowered vpaths for fuzzy find
	findList  kit.List
	openPath  string // absolute path of opened file
	openVPath string

	bufs      []*bufTab
	activeTab int
	bufFocus  bool
	// split panes (F-057): the second pane's tab, the pending ctrl+w
	// prefix, and a render flag that keeps popups on the focused pane.
	split          bool
	splitTab       int
	pendingW       bool
	renderingSplit bool

	// agentEditPath/agentEditAt mark the last agent-applied edit so the
	// buffer title can show an active-editing indicator (F-035 Part B).
	agentEditPath string
	agentEditAt   time.Time

	// Pair programming (F-038): agent suggestions awaiting review.
	proposals    []proposal
	reviewOpen   bool
	pairNote     string
	breakpoints  map[string][]int // abs path → sorted 1-based lines (F-039)
	dbg          *dbgState
	dapStart     dapStarter    // adapter launcher; nil = delve (tests inject)
	testing      *testState    // :test results (modeTests)
	syms         *symbolPicker // :sym outline picker (modeSymbols)
	formatOnSave bool          // :set fmt|nofmt; default on (Go + live server only)
	pairAgent    string        // pairing partner (F-038); "" = no session

	drawerOpen  bool
	termFocus   bool
	terms       []*termTab
	activeTerm  int
	cancelTerms []context.CancelFunc
	termMsgs    chan teaMsg
	termEnv     []string

	memEvents chan struct{} // workspace roster pings (P1 re-resolution)
	memCancel func()

	previewOn  bool
	previewKey string // content hash of last rendered preview
	previewDoc string

	gitOpen      bool
	gitFocus     bool
	gitTab       int // 0 status, 1 log
	gitCursor    int
	gitRepo      *gitcore.Repo
	gitIdentity  gitcore.IdentityFunc // F-029 commit identity; nil refuses
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
	queued        tea.Cmd // drained by the shell after a key (surfaces.CmdSource)
	searchCancel  context.CancelFunc
	searching     bool
	searchErr     string
	lastQueryText string
	// F-056: regex search mode (ctrl+r in the query box), the mode the
	// shown results used, the open replace prompt and its last outcome.
	searchRegex    bool
	lastQueryRegex bool
	replace        *replaceState
	replaceNote    string

	lspMgr        *lsp.Manager
	emit          func(event string) // tutorial action events (F-051); nil = not wired
	lspNoted      map[string]bool    // languages already told about a missing server (F-011, F-050)
	langs         langserver.Registry
	lspInstaller  LSPInstaller
	lspInstalling map[string]bool   // language id → install in flight
	lspSent       map[string]string // vpath → last pushed text
	lspDiags      map[string][]lsp.Diagnostic
	compOpen      bool
	compItems     []lsp.CompletionItem
	compCur       int
	lspPendingG   bool // `g` prefix armed (F-009 sequences)
	hoverOpen     bool
	hoverLines    []string
	renameMode    bool
	renameOld     string
	renameInput   []rune
	actionOpen    bool
	actionItems   []lsp.CodeAction
	actionCur     int

	chat *chatModel
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
		version:       version,
		ws:            ws,
		termMsgs:      make(chan teaMsg, 128),
		memEvents:     make(chan struct{}, 8),
		lspSent:       map[string]string{},
		lspNoted:      map[string]bool{},
		lspInstalling: map[string]bool{},
		lspDiags:      map[string][]lsp.Diagnostic{},

		formatOnSave: true,
	}
	m.langs, _ = langserver.Resolve(nil) // built-ins until WithLanguages says otherwise
	if ws != nil {
		for _, mem := range ws.Members() {
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

// StatusContext feeds the app statusline (F-025): which zone owns the
// keys, and the live mode chip (buffer modal states, finder, chat).
func (m *Model) StatusContext() (string, string) {
	switch {
	case m.reviewOpen && len(m.proposals) > 0:
		return "suggestion", "REVIEW"
	case m.chat != nil && m.chat.open && m.chat.focus:
		return "crew", "CHAT"
	case m.mode == modeFind:
		return "files", "FIND"
	case m.mode == modeSearchQuery:
		return "search", "SEARCH"
	case m.mode == modeResults:
		return "results", ""
	case m.mode == modeSymbols:
		return "symbols", "SYMBOLS"
	case m.mode == modeTests:
		return "tests", "TESTS"
	case m.mode == modeDebug:
		return "debugger", "DEBUG"
	case m.drawerOpen && m.termFocus:
		return "terminal", "TERM"
	case m.gitOpen && m.gitFocus:
		return "git", "GIT"
	case m.bufFocus && m.active() != nil:
		switch m.active().Mode() {
		case textbuf.ModeInsert:
			return "buffer", "INSERT"
		case textbuf.ModeVisual:
			return "buffer", "VISUAL"
		}
		return "buffer", ""
	}
	return "files", ""
}

// CapturesInput implements surfaces.InputCapturer. A focused buffer owns
// every plain key (insert text, vim counts like 3dd, tab); so do the
// terminal, chat composer, git panel and the file/search/symbol prompts.
// Leave the buffer with esc to get the digit view-switch keys back, or
// use the command palette from anywhere.
func (m *Model) CapturesInput() bool {
	switch {
	case m.drawerOpen && m.termFocus,
		m.chat != nil && m.chat.open && m.chat.focus,
		m.gitOpen && m.gitFocus:
		return true
	case m.mode == modeFind, m.mode == modeSearchQuery, m.mode == modeSymbols:
		return true
	case m.mode == modeResults && m.replace != nil:
		return true
	}
	return m.bufFocus && m.active() != nil
}

// Wheel routes wheel events to the focused pane (F-026 P2): three
// rows per tick, synthesized through the same key path as j/k — mode
// guards keep the wheel out of input surfaces (finder, composer,
// terminal, rename/action prompts, insert mode).
func (m *Model) Wheel(dy int) bool {
	if dy == 0 {
		return false
	}
	key := "k"
	if dy > 0 {
		key = "j"
	}
	switch {
	case m.mode == modeFind || m.mode == modeSearchQuery:
		return false
	case m.chat != nil && m.chat.open && m.chat.focus:
		return false
	case m.drawerOpen && m.termFocus:
		return false
	case m.renameMode || m.actionOpen:
		return false
	case m.bufFocus && m.active() != nil && m.active().Mode() != textbuf.ModeNormal:
		return false
	}
	scrolled := false
	for i := 0; i < 3; i++ {
		if m.HandleKey(key) {
			scrolled = true
		}
	}
	return scrolled
}

// HelpSections feeds the shell's contextual help (F-026 P7): the live
// mode's keys, wording matching the editor's own hint rows.
func (m *Model) HelpSections() [][2]string {
	out := [][2]string{
		{"enter", "open the selected file"},
		{"/", "find a file"},
		{"s", "search the workspace (ripgrep)"},
	}
	switch {
	case m.chat != nil && m.chat.open && m.chat.focus:
		out = append(out,
			[2]string{"enter", "send to the crew"},
			[2]string{"ctrl+f", "apply the suggestion block"})
	case m.drawerOpen && m.termFocus:
		out = append(out,
			[2]string{"ctrl+t", "blur/close the drawer"},
			[2]string{"alt+1..9", "switch terminal tab"})
	case m.gitOpen && m.gitFocus:
		out = append(out,
			[2]string{"s/S/u", "stage · unstage"},
			[2]string{"c", "commit"})
	case m.bufFocus && m.active() != nil:
		out = append(out,
			[2]string{"i / esc", "insert · back to normal"},
			[2]string{":w :q :wq :e", "save · quit · reload"},
			[2]string{"K gr ga", "hover · rename · code actions"},
			[2]string{":pair <agent>", "invite an agent to pair (:ask · :unpair)"},
			[2]string{":break · :debug", "breakpoint · start debugging (:cont :next :step :out :stop :eval)"},
			[2]string{":test [all|name]", "run Go tests · failures list jumps to the line"},
			[2]string{":fmt · :sym", "format · outline / go to symbol (format on save: :set nofmt)"},
			[2]string{"ctrl+y", "review agent suggestions"},
			[2]string{"ctrl+g", "markdown preview"},
			[2]string{":s/old/new/[g]", "replace"},
			[2]string{"ctrl+w v · ctrl+w w", "split side by side · switch pane (:vs · :only)"})
	case m.mode == modeResults:
		out = append(out, [2]string{"enter", "jump to the hit"},
			[2]string{"r", "replace across every hit (preview, then enter)"})
	}
	return out
}

// Init starts the terminal and chat message pumps plus the workspace
// roster watcher (live re-resolution without restart, P1).
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.listenTerm()}
	if m.ws != nil {
		ch, cancel := m.ws.Subscribe()
		m.memCancel = cancel
		go func() {
			for range ch {
				select {
				case m.memEvents <- struct{}{}:
				default:
				}
			}
		}()
		cmds = append(cmds, m.listenMembers())
	}
	if m.chat != nil {
		cmds = append(cmds, m.chat.start())
	}
	return tea.Batch(cmds...)
}

type membersChangedMsg struct{}

func (m *Model) listenMembers() tea.Cmd {
	ch := m.memEvents
	return func() tea.Msg {
		_, ok := <-ch
		if !ok {
			return nil
		}
		return membersChangedMsg{}
	}
}

// reloadMembers rebuilds the tree/search roots/fuzzy index from the
// current roster, closing buffers and terminal sessions that belonged
// to removed members. New members appear lazily (tree roots now;
// terminal tabs on next drawer open).
func (m *Model) reloadMembers() {
	if m.ws == nil {
		return
	}
	snap := m.ws.Members()
	aliveName := map[string]bool{}
	alivePath := map[string]bool{}
	m.members = nil
	for _, mem := range snap {
		aliveName[mem.Name] = true
		alivePath[mem.Path] = true
		m.members = append(m.members, memberRef{name: mem.Name, path: mem.Path})
	}
	m.roots = buildRoots(m.members)
	m.refreshRows()
	m.items = nil // fuzzy find reindexes lazily
	// Close terminals pinned to removed member dirs.
	var terms []*termTab
	var cancels []context.CancelFunc
	for i, t := range m.terms {
		if alivePath[t.dir] || t.exited {
			terms = append(terms, t)
			cancels = append(cancels, m.cancelTerms[i])
			continue
		}
		if m.cancelTerms[i] != nil {
			m.cancelTerms[i]()
		}
	}
	m.terms, m.cancelTerms = terms, cancels
	if m.activeTerm >= len(m.terms) {
		m.activeTerm = maxInt(len(m.terms)-1, 0)
	}

	// Close buffers whose vpath member vanished.
	var bufs []*bufTab
	for _, b := range m.bufs {
		member, _, _ := strings.Cut(b.vp, "/")
		if member == "" || aliveName[member] {
			bufs = append(bufs, b)
		} else if b.ed != nil {
			b.ed.SetCommandDelegate(nil)
		}
	}
	m.bufs = bufs
	if m.activeTab >= len(m.bufs) {
		m.activeTab = maxInt(len(m.bufs)-1, 0)
	}
	if len(m.bufs) == 0 {
		m.bufFocus = false
		// Drop stale identity so the tab strip stops rendering the
		// removed member's file.
		m.openPath, m.openVPath = "", ""
	}
	if m.list.Cursor > len(m.rows)-1 {
		m.list.Cursor = maxInt(len(m.rows)-1, 0)
	}
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
	kind      uint8 // termMsgOut | termMsgClosed | lspMsgDiag | lspMsgComp | lspMsgHover | lspMsgEdit | lspMsgAction
	tab       int
	chunk     []byte
	diags     []lsp.Diagnostic
	diagPath  string
	compItems []lsp.CompletionItem
	hoverText string
	hoverOK   bool
	edit      *lsp.WorkspaceEdit
	actions   []lsp.CodeAction
	note      string
	lang      string // lspMsgInstalled: the language id
	installOK bool   // lspMsgInstalled: the install succeeded
	testRep   *testrun.Report
	dbgStart  *dbgStarted
}

const (
	termMsgOut uint8 = iota
	termMsgClosed
	lspMsgDiag
	lspMsgComp
	lspMsgHover
	lspMsgEdit
	lspMsgAction
	lspMsgNote
	lspMsgInstalled // a confirmed `:lsp install` finished (note carries the outcome)
	testMsgDone
	dbgMsgStarted
	dbgMsgUpdate
)

func (m *Model) Resize(w, h int) {
	m.width, m.height = w, h
	m.list.Width = m.railW() - 4
	m.list.Height = h - 3
	m.list.Inset = true // shaded sidebar zone (F-025)
	m.findList.Width = 60 - 4
	m.findList.Height = min(12, h-6)
	m.hitList.Width = maxInt(w-m.railW()-7, 10)
	m.hitList.Height = h - 5

	if t := m.activeTermTab(); t != nil && t.sess != nil && !t.exited {
		rows := min(drawerHeight, maxInt(h/3, 4)) - 2
		_ = t.sess.Resize(maxInt(w-m.railW()-4, 20), rows)
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
	case membersChangedMsg:
		m.reloadMembers()
		return m.listenMembers()
	case teaMsg:
		switch msg.kind {
		case termMsgOut:
			m.ingestTermChunk(msg.tab, msg.chunk)
		case termMsgClosed:
			m.termExited(msg.tab)
		case lspMsgDiag, lspMsgComp, lspMsgHover, lspMsgEdit, lspMsgAction, lspMsgNote, lspMsgInstalled:
			m.applyLSPUpdate(msg)
		case testMsgDone:
			m.applyTestDone(msg.testRep)
		case dbgMsgStarted:
			m.applyDebugStarted(msg.dbgStart)
		case dbgMsgUpdate:
			m.applyDebugUpdate()
		}
		return m.listenTerm()
	case chatEvent:
		if msg.roster && m.chat != nil && !m.chat.closed() {
			m.chat.refreshRoster()
		}
		// Transcript renders straight from bus history; just re-arm.
		if m.chat != nil && !m.chat.closed() {
			return m.chat.listen()
		}
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

// TakeCmd implements surfaces.CmdSource: work a key started.
func (m *Model) TakeCmd() tea.Cmd {
	c := m.queued
	m.queued = nil
	return c
}

// HandleKey implements surface key routing.
func (m *Model) HandleKey(key string) bool {
	if m.ws == nil {
		return false
	}

	if m.reviewOpen && len(m.proposals) > 0 {
		return m.handleReviewKey(key)
	}
	if key == "ctrl+d" && m.dbg != nil && m.dbg.st.Stopped {
		m.mode = modeDebug
		return true
	}
	if key == "ctrl+y" && len(m.proposals) > 0 {
		m.reviewOpen = true
		return true
	}
	if key == "ctrl+t" {
		m.ToggleDrawer()
		return true
	}
	if key == "ctrl+j" {
		m.ToggleGitPanel()
		return true
	}
	if key == "ctrl+a" && m.chat != nil {
		m.chat.Toggle()
		return true
	}
	if m.chat != nil && m.chat.open && m.chat.focus {
		if handled := m.chat.handleKey(key, m.applySuggestion); handled {
			return true
		}
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
	case modeSymbols:
		return m.handleSymbolKey(key)
	case modeTests:
		return m.handleTestKey(key)
	case modeDebug:
		return m.handleDebugKey(key)
	}

	if m.bufFocus && m.active() != nil {
		e := m.active()

		// completion popup intercepts navigation/accept keys
		if m.compOpen {
			if handled := m.handleCompletionKey(key); handled || key == "esc" {
				return true
			}
		}

		// F-009 LSP flows intercept before the modal editor sees keys.
		if m.renameMode {
			return m.handleRenameKey(key)
		}
		if m.actionOpen {
			if handled := m.handleActionKey(key); handled || key == "esc" {
				return true
			}
		}
		if m.hoverOpen {
			m.hoverOpen = false
			m.hoverLines = nil
			if key != "esc" {
				e.Key(key) // close-and-process (movement etc.)
				m.lspSync()
			}
			return true
		}

		// ctrl+w window prefix (F-057), normal mode only: insert-mode
		// ctrl+w stays the buffer's.
		if e.Mode() == textbuf.ModeNormal {
			if m.pendingW {
				m.pendingW = false
				m.splitKey(key)
				return true
			}
			if key == "ctrl+w" {
				m.pendingW = true
				return true
			}
		}

		// `gr`/`ga` sequences and `K` hover (normal mode only; `g` is
		// dead in textbuf so the surface owns the two-key chords).
		if m.lspPendingG {
			if e.Mode() == textbuf.ModeNormal {
				m.lspPendingG = false
				switch key {
				case "esc":
					// release the prefix and let esc fall through
				case "r":
					m.startRename()
					return true
				case "a":
					m.requestCodeActions()
					return true
				default:
					return true // unknown g-sequence swallowed
				}
			} else {
				m.lspPendingG = false // mode changed; release
			}
		} else if e.Mode() == textbuf.ModeNormal && key == "g" {
			m.lspPendingG = true
			return true
		}
		if e.Mode() == textbuf.ModeNormal && key == "K" {
			m.requestHover()
			return true
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

		was := e.Mode()
		e.Key(key)
		if e.Mode() == textbuf.ModeInsert && was != textbuf.ModeInsert {
			m.act(tutorial.EvEditorInsert)
		}
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
	case "space", " ":
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
	ed     *textbuf.Editor
	vp     string
	path   string
	syntax *highlighter // per-tab buffer colorizer (F-026 P4)
	gutter gutterState  // git change markers (F-040)
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
			m.act(tutorial.EvEditorOpen)
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
	be.SetBeforeSave(m.beforeSave)
	be.SetAfterSave(func(*textbuf.Editor) { m.act(tutorial.EvEditorSave) })
	tab := &bufTab{ed: be, vp: m.openVPath, path: n.path, syntax: &highlighter{path: n.path}}
	m.bufs = append(m.bufs, tab)
	m.activeTab = len(m.bufs) - 1
	m.bufFocus = true
	m.lspOpenDoc(n.path, be.Buffer().Text())
	m.act(tutorial.EvEditorOpen)
}

// closeBuffer drops the active tab; focus lands on the neighbor or the
// tree when none remain.
func (m *Model) closeBuffer() {
	if m.activeTab >= len(m.bufs) {
		m.bufFocus = false
		return
	}
	closing := m.activeTab
	m.bufs = append(m.bufs[:m.activeTab], m.bufs[m.activeTab+1:]...)
	switch {
	case len(m.bufs) == 0:
		m.bufFocus = false
	case m.activeTab >= len(m.bufs):
		m.activeTab = len(m.bufs) - 1
	}
	if m.split { // closing a pane's buffer ends the split on the other one
		m.split = false
		other := m.splitTab
		if other > closing {
			other--
		}
		if other >= 0 && other < len(m.bufs) && other != closing {
			m.activeTab = other
		}
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
		return branding.NoWorkspace(m.width, m.height, m.version)
	}
	if m.reviewOpen && len(m.proposals) > 0 {
		return m.reviewView()
	}
	if m.mode == modeSymbols && m.syms != nil {
		return m.symbolsView()
	}
	if m.mode == modeTests && m.testing != nil {
		return m.testsView()
	}
	if m.mode == modeDebug && m.dbg != nil {
		return m.debugView()
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

// Click implements the shell's click seam (F-055): a file-tree row is
// selected (and the tree focused); clicking the selected row again opens
// the file or toggles the folder, like enter.
func (m *Model) Click(x, y int) bool {
	if m.ws == nil || m.mode != modeNav || m.reviewOpen {
		return false
	}
	railW := m.railW()
	if railW == 0 || x >= railW {
		return false
	}
	i, ok := m.list.RowAt(y - 1) // panel top border
	if !ok {
		return false
	}
	m.bufFocus = false
	if m.list.Cursor == i {
		m.HandleKey("enter")
		return true
	}
	m.list.Cursor = i
	return true
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
	hint := theme.Hint().Render(kit.ClipEllipsis("⏎ open · / find · s search", m.railW()-4))
	if m.railW() < 32 {
		hint = theme.Hint().Render("⏎ open · / find") // s search: see ? help
	}
	savedListH := m.list.Height
	m.list.Width = maxInt(m.railW()-4, 8)
	m.list.Height = bodyH - 4 // hint row, spacer, panel padding
	content := splitLines(m.list.View())
	sb := m.list.Scroller() // capture with the live window height (F-025)
	for len(content) < bodyH-4 {
		content = append(content, "") // push hints to the rail's foot
	}
	rail.SetContent(append(content, "", hint)...)
	rail.Width = maxInt(m.railW(), 1)
	rail.Height = bodyH
	rail.SetScroll(sb)
	m.list.Height = savedListH

	var main string
	title := mainTitle(m.openVPath)
	switch {
	case m.mode == modeResults: // search results win over an open buffer
		main = m.resultsBlock()
		title = "results"
	case m.active() != nil && m.previewOn && preview.IsMarkdown(m.active().Path()):
		main = m.previewView()
		title = "preview — " + title
	case m.active() != nil:
		e := m.active()
		title = bufferTitle(e) + m.diagChip(e) + m.agentChip(e) + m.pairBadge() + m.pairChip(e.Path())
		main = m.bufferView()
	case m.openPath != "":
		main = strings.Join([]string{
			theme.TabActive().Render(m.openVPath),
			"",
			theme.Hint().Render("press enter on a file to edit"),
		}, "\n")
	default:
		main = strings.Join([]string{
			theme.Brand().Render("no file open"),
			"",
			theme.Hint().Render("⏎ open  ·  / find file  ·  s search"),
		}, "\n")
	}

	railW := m.railW()
	if railW >= m.width { // narrow, nothing open: the tree is the view
		return m.withBottomPanels(rail.View())
	}
	mainW := m.width - railW - 1
	if railW == 0 {
		mainW = m.width
	}
	chatOpen := m.chat != nil && m.chat.open
	if chatOpen {
		mainW -= chatWidth // the crew panel docks right; the buffer gives way
	}
	mainW = maxInt(mainW, 10)
	centered := kit.Center(main, maxInt(mainW-2, 10), maxInt(bodyH-2, 3))
	if m.mode == modeResults || m.active() != nil {
		centered = main // lists and buffers are left-aligned
	}
	if m.active() != nil && m.mode != modeResults {
		centered = joinV(tabStrip(m.bufs, m.activeTab, mainW-2), centered)
	}
	mainPanel := kit.NewPanel(title, true)
	mainPanel.SetContent(splitLines(centered)...)
	mainPanel.Width = mainW
	mainPanel.Height = bodyH

	out := mainPanel.View()
	mdPreview := m.previewOn && m.active() != nil && preview.IsMarkdown(m.active().Path())
	if m.mode != modeResults && !mdPreview && m.splitActive() {
		if panes := m.splitPanels(mainW, bodyH); panes != "" {
			out = panes
		}
	}
	if railW > 0 {
		out = joinH(rail.View(), out)
	}
	if chatOpen {
		chatPanel := kit.NewPanel("crew", true)
		body := m.chat.view(bodyH)
		chatPanel.SetContent(splitLines(body)...)
		chatPanel.Width = chatWidth
		chatPanel.Height = bodyH
		out = joinH(out, chatPanel.View())
	}
	return m.withBottomPanels(out)
}

// withBottomPanels stacks the git panel and the terminal drawer under
// the main row when they are open.
func (m *Model) withBottomPanels(out string) string {
	if m.gitOpen {
		out += "\n" + m.gitPanelView()
	}
	if m.drawerOpen {
		out += "\n" + m.drawerView()
	}
	return out
}

// railW is the file tree's width for the current terminal (F-054): it
// shrinks with the window, and below the compact breakpoint only one
// pane shows — the open buffer (rail 0) or, with nothing open, the
// tree at full width.
func (m *Model) railW() int {
	switch {
	case m.width >= kit.WWide:
		return 34
	case m.width >= kit.WDock:
		return 28
	case m.width >= kit.WCompact:
		return 24
	case m.active() != nil:
		return 0
	}
	return m.width
}

// ExecEx implements textbuf.CommandDelegate: buffer-list ex commands.
func (m *Model) ExecEx(requester *textbuf.Editor, cmd string) bool {
	if msg, ok := m.splitCommand(cmd); ok {
		requester.SetMessage(msg)
		return true
	}
	if msg, ok := m.pairCommand(cmd); ok {
		requester.SetMessage(msg)
		if strings.HasPrefix(cmd, "pair") && m.pairAgent != "" {
			m.act(tutorial.EvEditorPair)
		}
		return true
	}
	if msg, ok := m.lspCommand(cmd); ok {
		requester.SetMessage(msg)
		return true
	}
	if msg, ok := m.fmtCommand(cmd); ok {
		requester.SetMessage(msg)
		if strings.TrimSpace(cmd) == "fmt" && msg == "formatted" {
			m.act(tutorial.EvEditorFormat)
		}
		return true
	}
	if msg, ok := m.symCommand(cmd); ok {
		requester.SetMessage(msg)
		return true
	}
	if msg, ok := m.testCommand(cmd); ok {
		requester.SetMessage(msg)
		if msg == "running tests…" {
			m.act(tutorial.EvEditorTest)
		}
		return true
	}
	if msg, ok := m.debugCommand(cmd); ok {
		requester.SetMessage(msg)
		if strings.HasPrefix(cmd, "break") && strings.HasPrefix(msg, "breakpoint set") {
			m.act(tutorial.EvEditorBreak)
		}
		return true
	}
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
		m.items = fuzzy.NewIndex(indexFiles(m.roots, indexCap))
	}
	m.query = nil
	m.mode = modeFind
	m.applyQuery()
}

func (m *Model) applyQuery() {
	pat := string(m.query)
	m.results = m.items.Rank(pat)
	limit := len(m.results)
	if limit > findCapRows {
		limit = findCapRows
	}
	index := m.items.Items()
	items := make([]kit.Item, 0, limit)
	for _, r := range m.results[:limit] {
		items = append(items, kit.Item{Title: index[r.Index]})
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

// OpenPaths opens each absolute path in its own buffer, revealing it in
// the nav tree, and reports how many buffers opened. It is the
// cross-surface handoff seam used by the Reviewer's "open in editor"
// completion flow (F-005).
// OpenPaths opens files preloaded and focused.
func (m *Model) OpenPaths(paths ...string) int {
	opened := 0
	for _, p := range paths {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		revealTo(m.roots, abs)
		m.refreshRows()
		m.open(&node{kind: nodeFile, path: abs, name: filepath.Base(abs)})
		opened++
	}
	return opened
}

// FocusChat opens the chat sidebar focused (F-016 approval jump). False
// when this editor has no chat (no runtime wired).
func (m *Model) FocusChat() bool {
	if m.chat == nil {
		return false
	}
	m.chat.Focus()
	return true
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
	vpathStr := m.items.Items()[m.results[idx].Index]
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
	case "ctrl+r":
		m.searchRegex = !m.searchRegex
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
	m.lastQueryRegex = m.searchRegex
	m.replace, m.replaceNote = nil, ""
	m.mode = modeResults

	roots := make([]string, len(m.members))
	for i, mem := range m.members {
		roots[i] = mem.path
	}
	var ch <-chan search.Hit
	var err error
	if m.searchRegex {
		rx, ok := m.searcher.(search.RegexSearcher)
		if !ok {
			err = fmt.Errorf("regex search is not available here (ctrl+r for fixed-string)")
		} else {
			ch, err = rx.SearchRegex(ctx, q, roots)
		}
	} else {
		ch, err = m.searcher.Search(ctx, q, roots)
	}
	if err != nil {
		m.searching = false
		m.hitsCh = nil
		m.searchErr = err.Error()
		return
	}
	m.searchErr = ""
	m.searching = true
	m.hitsCh = ch
	m.queued = m.listenHits() // the shell pumps the stream from here
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
	if m.replace != nil {
		return m.handleReplaceKey(key)
	}
	switch key {
	case "esc":
		m.cancelSearch()
		m.mode = modeNav
		return true
	case "enter", "l":
		return m.jumpHit()
	case "r":
		m.openReplace()
		return true
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
	if m.replace != nil {
		return m.replaceBlock(maxInt(m.width-m.railW()-8, 20))
	}
	head := theme.TabActive().Render("results for " + strconv.Quote(m.lastQueryText))
	if m.lastQueryRegex {
		head += theme.Hint().Render(" (regex)")
	}
	if m.replaceNote != "" {
		head += "\n" + theme.SuccessText().Render(theme.GlyphCheck+" "+m.replaceNote)
	}
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
	lines = append(lines, "", theme.Hint().Render("⏎ jump · r replace · esc back"))
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
	mode := "fixed-string search across all member repos · ctrl+r regex"
	if m.searchRegex {
		mode = "regex search across all member repos · ctrl+r fixed-string"
	}
	body := []string{
		theme.Brand().Render(head),
		"",
		theme.TextDim().Render(mode),
	}
	overlay := kit.NewPanel("search", true)
	overlay.SetContent(body...)
	overlay.Width = 64
	overlay.Height = min(8, m.height)

	dim := theme.Hint().Render("enter search · esc cancel")
	return kit.Center(joinV(overlay.View(), "", dim), m.width, m.height)
}

// applySuggestion inserts text at the cursor of the active buffer, if any.
// ApplyReplace edits a file the way the editor would: through the live
// buffer when the file is open (one undo step, saved by the human),
// else the file on disk. Refuses absent/ambiguous matches unless all
// (ADR-0023 editor_apply_edit).
func (m *Model) ApplyReplace(abs, old, new string, all bool) error {
	for _, t := range m.bufs {
		if t.path == abs {
			_, err := t.ed.Buffer().ReplaceText(old, new, all)
			if err != nil {
				return err
			}
			m.markAgentEdit(abs)
			return nil
		}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	content := string(data)
	count := strings.Count(content, old)
	switch {
	case count == 0:
		return fmt.Errorf("old text not found in %s", abs)
	case count > 1 && !all:
		return fmt.Errorf("old text appears %d times in %s (set replace_all or narrow it)", count, abs)
	}
	var replaced string
	if all {
		replaced = strings.ReplaceAll(content, old, new)
	} else {
		replaced = strings.Replace(content, old, new, 1)
	}
	if err := os.WriteFile(abs, []byte(replaced), 0o644); err != nil {
		return err
	}
	m.markAgentEdit(abs)
	return nil
}

// markAgentEdit stamps the last agent-applied edit so the title shows the
// active-editing indicator (F-035 Part B).
func (m *Model) markAgentEdit(abs string) {
	m.agentEditPath = abs
	m.agentEditAt = time.Now()
}

// agentChip is the active-editing indicator appended to the buffer title:
// non-empty only while e is the buffer an agent just edited.
func (m *Model) agentChip(e *textbuf.Editor) string {
	if e == nil || m.agentEditPath == "" {
		return ""
	}
	if e.Path() != m.agentEditPath {
		return ""
	}
	if time.Since(m.agentEditAt) > agentEditWindow {
		return ""
	}
	return "  " + theme.SuccessText().Render("● agent editing")
}

func (m *Model) applySuggestion(text string) {
	e := m.active()
	if e == nil || text == "" {
		return
	}
	b := e.Buffer()
	b.BeginUndoGroup()
	for _, line := range strings.Split(text, "\n") {
		b.InsertString(line)
		b.InsertString("\n")
	}
	b.EndUndoGroup()
	m.lspSync()
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
