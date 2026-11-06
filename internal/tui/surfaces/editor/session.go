package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Session restore (F-149). On quit the open tabs, the active one and the
// pane layout are written to the workspace's .dhi/tmp/session.json (per user,
// ignored by git, next to the recent-files list); on the next launch in
// the same workspace they reopen. Unsaved text is not part of it (hot
// exit keeps it in swap files, F-161). The scroll position is not stored: the buffer view
// derives it from the cursor, so restoring the cursor restores the view.

const sessionVersion = 1

type sessionTab struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type sessionFile struct {
	Version int          `json:"version"`
	Tabs    []sessionTab `json:"tabs"`
	Active  string       `json:"active,omitempty"` // path of the focused tab
	Split   string       `json:"split,omitempty"`  // the old two-pane form: the other pane's tab; read when layout is absent
	Layout  *sessionPane `json:"layout,omitempty"` // the pane tree (F-166); nil = one pane
}

// sessionPane is one node of the saved pane tree: a leaf names a tab's
// file, a node lists its kids and how they are laid out ("cols" side by
// side, "rows" stacked).
type sessionPane struct {
	Dir   string        `json:"dir,omitempty"`
	Kids  []sessionPane `json:"kids,omitempty"`
	Path  string        `json:"path,omitempty"`
	Focus bool          `json:"focus,omitempty"`
}

func (m *Model) sessionPath() string {
	if m.ws == nil {
		return ""
	}
	return filepath.Join(m.ws.Root, workspace.DHIDir, "tmp", "session.json")
}

// SaveSession implements surfaces.SessionSaver: it writes the session as
// DHI quits. A failed write is not worth stopping the quit for.
func (m *Model) SaveSession() {
	path := m.sessionPath()
	if path == "" || !m.editorPrefs().RestoreSession {
		return
	}
	s := sessionFile{Version: sessionVersion, Tabs: []sessionTab{}}
	for _, t := range m.bufs {
		if t.path == "" || t.ed == nil || within(t.path, filepath.Join(m.ws.Root, workspace.DHIDir)) {
			continue // scratch and DHI's own buffers (a commit message) are not session
		}
		c := t.ed.Buffer().Cursor()
		s.Tabs = append(s.Tabs, sessionTab{Path: t.path, Line: c.Line, Col: c.Col})
	}
	if m.activeTab < len(m.bufs) {
		s.Active = m.bufs[m.activeTab].path
	}
	if m.splitActive() {
		s.Layout = m.sessionLayout(m.layout)
		if l := m.layout.leaves(); len(l) == 2 { // older DHI reads the two-pane field
			for _, p := range l {
				if p != m.focus {
					s.Split = p.tab.path
				}
			}
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
}

// sessionLayout copies the pane tree, leaving out panes whose tab is not
// part of a session. A node left with one kid becomes that kid.
func (m *Model) sessionLayout(p *pane) *sessionPane {
	if p.leaf() {
		if p.tab.path == "" || within(p.tab.path, filepath.Join(m.ws.Root, workspace.DHIDir)) {
			return nil
		}
		return &sessionPane{Path: p.tab.path, Focus: p == m.focus}
	}
	n := &sessionPane{Dir: "cols"}
	if p.dir == paneRows {
		n.Dir = "rows"
	}
	for _, k := range p.kids {
		if c := m.sessionLayout(k); c != nil {
			n.Kids = append(n.Kids, *c)
		}
	}
	switch len(n.Kids) {
	case 0:
		return nil
	case 1:
		return &n.Kids[0]
	}
	return n
}

// restorePanes rebuilds a saved pane tree from the tabs that came back;
// ok is false when fewer than two panes survive.
func (m *Model) restorePanes(s sessionFile) (root, focus *pane, ok bool) {
	var build func(n sessionPane) *pane
	build = func(n sessionPane) *pane {
		if len(n.Kids) == 0 {
			t := m.tabFor(n.Path)
			if t == nil {
				return nil
			}
			p := &pane{tab: t}
			if n.Focus {
				focus = p
			}
			return p
		}
		p := &pane{dir: paneCols}
		if n.Dir == "rows" {
			p.dir = paneRows
		}
		for _, k := range n.Kids {
			if c := build(k); c != nil {
				p.kids = append(p.kids, c)
			}
		}
		switch len(p.kids) {
		case 0:
			return nil
		case 1:
			return p.kids[0]
		}
		return p
	}
	switch {
	case s.Layout != nil:
		root = build(*s.Layout)
	case s.Split != "": // sessions saved before the pane tree
		a, b := m.tabFor(s.Active), m.tabFor(s.Split)
		if a != nil && b != nil && a != b {
			root = &pane{kids: []*pane{{tab: a}, {tab: b}}}
			focus = root.kids[0]
		}
	}
	leaves := root.leaves()
	if len(leaves) < 2 || len(leaves) > maxPanes {
		return nil, nil, false
	}
	if focus == nil {
		focus = leaves[0]
		for _, l := range leaves {
			if l.tab == m.tabFor(s.Active) {
				focus = l
				break
			}
		}
	}
	return root, focus, true
}

// restoreSession reopens the saved tabs. Files that are gone, and files
// over the large-file threshold (F-138), are skipped: a launch never
// shows a confirmation, and a 5 MiB buffer is not worth loading before the
// user asks for it. One toast lists what was skipped.
func (m *Model) restoreSession() {
	path := m.sessionPath()
	if path == "" || !m.editorPrefs().RestoreSession {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return // no session yet
	}
	var s sessionFile
	if err := json.Unmarshal(data, &s); err != nil || s.Version != sessionVersion {
		m.sessionToast("the saved session in .dhi/tmp/session.json is unreadable and was ignored")
		return
	}
	var missing, large []string
	m.restoring = true
	defer func() { m.restoring = false }()
	for _, t := range s.Tabs {
		switch {
		case !fileExists(t.Path):
			missing = append(missing, m.relToWorkspace(t.Path))
			continue
		case m.tabFor(t.Path) != nil:
			continue
		}
		if _, big := largeTextFile(t.Path); big {
			large = append(large, m.relToWorkspace(t.Path))
			continue
		}
		revealTo(m.roots, t.Path)
		m.open(&node{kind: nodeFile, path: t.Path, name: filepath.Base(t.Path)})
		if tab := m.tabFor(t.Path); tab != nil {
			tab.ed.Buffer().SetCursor(textbuf.Pos{Line: t.Line, Col: t.Col})
		}
	}
	m.refreshRows()
	m.pickRestoredTabs(s)
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "gone: "+strings.Join(missing, ", "))
	}
	if len(large) > 0 {
		parts = append(parts, "over 5 MiB, open them yourself: "+strings.Join(large, ", "))
	}
	if len(parts) > 0 {
		m.sessionToast("not restored · " + strings.Join(parts, " · "))
	}
}

// pickRestoredTabs focuses the saved active tab and rebuilds the split
// when both of its buffers came back. Nothing is focused when no tab
// reopened: the start page stays.
func (m *Model) pickRestoredTabs(s sessionFile) {
	if len(m.bufs) == 0 {
		return
	}
	m.activeTab = len(m.bufs) - 1
	if t := m.tabFor(s.Active); t != nil {
		m.activeTab = m.indexOfTab(t)
	}
	m.openPath, m.openVPath = m.bufs[m.activeTab].path, m.bufs[m.activeTab].vp
	m.bufFocus = true
	if root, focus, ok := m.restorePanes(s); ok {
		m.layout, m.focus = root, focus
		m.splitActive() // the focused pane shows the active tab
	}
}

func (m *Model) indexOfTab(t *bufTab) int {
	for i, b := range m.bufs {
		if b == t {
			return i
		}
	}
	return -1
}

func (m *Model) sessionToast(detail string) {
	m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Session", Detail: detail}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
