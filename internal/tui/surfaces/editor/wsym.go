package editor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/fuzzy"
	"github.com/drjzlyan/dhi/internal/lsp"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Workspace symbols (F-154): `:wsym [query]` asks every running language
// server for workspace/symbol. Typing re-queries (debounced, async; a reply
// for an older query is dropped by sequence) and ranks the hits locally;
// enter opens the file at the symbol.

const (
	wsymDebounce = 150 * time.Millisecond
	wsymShown    = 100
)

type wsymRow struct {
	sym   lsp.WorkspaceSymbol
	vp    string // member/rel path
	label string // Container.Name, what the query is matched against
}

type wsymPicker struct {
	clients []*lsp.Client
	query   []rune
	cur     int
	seq     int // newest query; replies carrying another seq are stale
	waiting bool
	err     string
	rows    []wsymRow // every member hit for the last answered query
	shown   []wsymRow // rows ranked against the query
}

// wsymTickMsg fires after the debounce; wsymMsg carries one answer.
type wsymTickMsg struct{ seq int }
type wsymMsg struct {
	seq  int
	syms []lsp.WorkspaceSymbol
	err  error
}

// wsymCommand handles :wsym [query]. On success the picker opens
// (message "") and the first query is queued.
func (m *Model) wsymCommand(cmd string) (string, bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd != "wsym" && !strings.HasPrefix(cmd, "wsym ") {
		return "", false
	}
	if m.restricted(m.currentGitRootOrFirst(), "search workspace symbols") {
		return trustLine("search workspace symbols"), true
	}
	var clients []*lsp.Client
	if m.lspMgr != nil {
		for _, lc := range m.lspMgr.Live() {
			clients = append(clients, lc.Client)
		}
	}
	if len(clients) == 0 {
		return "wsym: no language server is running (open a source file to start one)", true
	}
	p := &wsymPicker{clients: clients, query: []rune(strings.TrimSpace(strings.TrimPrefix(cmd, "wsym")))}
	m.wsym, m.mode = p, modeWorkspaceSyms
	m.queue(p.ask())
	return "", true
}

// ask starts a new query generation and returns the command that runs it.
func (p *wsymPicker) ask() tea.Cmd {
	p.seq++
	p.waiting = true
	return p.run(p.seq)
}

func (p *wsymPicker) run(seq int) tea.Cmd {
	q, clients := string(p.query), p.clients
	return func() tea.Msg {
		var all []lsp.WorkspaceSymbol
		var firstErr error
		ok := 0
		for _, c := range clients {
			syms, err := withTimeout(func() ([]lsp.WorkspaceSymbol, error) { return c.WorkspaceSymbols(q) })
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			ok++
			all = append(all, syms...)
		}
		if ok > 0 {
			firstErr = nil // one server answering is an answer
		}
		return wsymMsg{seq: seq, syms: all, err: firstErr}
	}
}

// debounce schedules a query for a new seq; typing again supersedes it.
func (p *wsymPicker) debounce() tea.Cmd {
	p.seq++
	p.waiting = true
	seq := p.seq
	return tea.Tick(wsymDebounce, func(time.Time) tea.Msg { return wsymTickMsg{seq: seq} })
}

func (m *Model) onWsymTick(msg wsymTickMsg) tea.Cmd {
	p := m.wsym
	if m.mode != modeWorkspaceSyms || p == nil || msg.seq != p.seq {
		return nil
	}
	return p.run(msg.seq)
}

func (m *Model) onWsym(msg wsymMsg) {
	p := m.wsym
	if m.mode != modeWorkspaceSyms || p == nil || msg.seq != p.seq {
		return // closed, or a newer query is in flight
	}
	p.waiting = false
	p.err = ""
	if msg.err != nil {
		p.err = "workspace symbols failed: " + msg.err.Error()
	}
	p.rows = nil
	// Only symbols inside a workspace member: servers also index the
	// standard library and dependencies.
	for _, s := range msg.syms {
		vp, ok := m.memberPath(s.Path)
		if !ok {
			continue
		}
		label := s.Name
		if s.Container != "" {
			label = s.Container + "." + s.Name
		}
		p.rows = append(p.rows, wsymRow{sym: s, vp: vp, label: label})
	}
	p.rank()
}

// rank orders rows by fuzzy score against the query; an empty query keeps
// the server's order.
func (p *wsymPicker) rank() {
	p.cur = 0
	if len(p.query) == 0 {
		p.shown = capRows(p.rows)
		return
	}
	labels := make([]string, len(p.rows))
	for i, r := range p.rows {
		labels[i] = r.label
	}
	res := fuzzy.Rank(string(p.query), labels)
	sort.SliceStable(res, func(i, j int) bool { return res[i].Score > res[j].Score })
	out := make([]wsymRow, 0, len(res))
	for _, r := range res {
		out = append(out, p.rows[r.Index])
	}
	p.shown = capRows(out)
}

func capRows(r []wsymRow) []wsymRow {
	if len(r) > wsymShown {
		return r[:wsymShown]
	}
	return r
}

func (m *Model) handleWsymKey(key string) bool {
	p := m.wsym
	if p == nil {
		m.mode = modeNav
		return false
	}
	switch key {
	case "esc":
		m.mode, m.wsym = modeNav, nil
	case "enter":
		m.mode, m.wsym = modeNav, nil
		if p.cur < len(p.shown) {
			s := p.shown[p.cur].sym
			m.gotoLocation(s.Path, lsp.Position{Line: s.Line, Character: s.Col})
			m.bufFocus = true
		}
	case "down", "ctrl+n":
		if p.cur < len(p.shown)-1 {
			p.cur++
		}
	case "up", "ctrl+k":
		if p.cur > 0 {
			p.cur--
		}
	default:
		if q, ok := kit.EditQuery(string(p.query), key); ok {
			p.query = []rune(q)
			p.rank() // re-rank what is already here while the server answers
			m.queue(p.debounce())
		}
	}
	return true
}

func (m *Model) wsymView() string {
	p := m.wsym
	body := []string{theme.Brand().Render("> " + string(p.query) + "▌"), ""}
	switch {
	case p.err != "":
		body = append(body, theme.TextDim().Render("  "+p.err))
	case len(p.shown) == 0 && p.waiting:
		body = append(body, theme.TextDim().Render("  searching…"))
	case len(p.shown) == 0:
		body = append(body, theme.TextDim().Render("  no matching symbols"))
	}
	start := 0
	const window = 14
	if p.cur >= window {
		start = p.cur - window + 1
	}
	for i := start; i < len(p.shown) && i < start+window; i++ {
		r := p.shown[i]
		row := fmt.Sprintf("%-9s %s", r.sym.Kind, r.label)
		where := theme.TextDim().Render(fmt.Sprintf("%s:%d", r.vp, r.sym.Line+1))
		if i == p.cur {
			body = append(body, theme.Chip().Render(" "+row+" ")+"  "+where)
		} else {
			body = append(body, row+"  "+where)
		}
	}
	box := kit.NewPanel("workspace symbols", true)
	box.SetContent(body...)
	box.Width = 72
	box.Height = min(len(body)+2, m.height)
	hint := theme.Hint().Render("enter open · ↑/↓ move · type to search · esc cancel")
	return kit.Center(joinV(box.View(), "", hint), m.width, m.height)
}
