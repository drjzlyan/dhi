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
// split are written to the workspace's .dhi/tmp/session.json (per user,
// ignored by git, next to the recent-files list); on the next launch in
// the same workspace they reopen. Unsaved text is not part of it (hot
// exit is G-R2). The scroll position is not stored: the buffer view
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
	Split   string       `json:"split,omitempty"`  // path of the other pane's tab; "" = one pane
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
		s.Split = m.bufs[m.splitTab].path
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
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
	if t := m.tabFor(s.Split); t != nil && m.indexOfTab(t) != m.activeTab {
		m.split, m.splitTab = true, m.indexOfTab(t)
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
