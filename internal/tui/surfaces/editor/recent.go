package editor

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Recent files and reopening closed tabs (F-119). The list lives in the
// workspace's .dhi/tmp (per user, ignored by git).

const recentMax = 10

func (m *Model) recentFile() string {
	if m.ws == nil {
		return ""
	}
	return filepath.Join(m.ws.Root, workspace.DHIDir, "tmp", "recent-files")
}

// loadRecent reads the list once.
func (m *Model) loadRecent() {
	if m.recentLoaded || m.ws == nil {
		return
	}
	m.recentLoaded = true
	data, err := os.ReadFile(m.recentFile())
	if err != nil {
		return
	}
	for _, p := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if p != "" {
			m.recent = append(m.recent, p)
		}
	}
}

// noteRecent moves path to the front and saves the list (best effort:
// the list is a convenience, a failed write is not worth a message).
func (m *Model) noteRecent(path string) {
	if m.restoring {
		return // reopening the last session keeps the list as it was (F-149)
	}
	if m.ws != nil && within(path, filepath.Join(m.ws.Root, workspace.DHIDir)) {
		return // DHI's own files (a commit message) are not "recent"
	}
	m.loadRecent()
	out := []string{path}
	for _, p := range m.recent {
		if p != path && len(out) < recentMax {
			out = append(out, p)
		}
	}
	m.recent = out
	if f := m.recentFile(); f != "" {
		if os.MkdirAll(filepath.Dir(f), 0o755) == nil {
			_ = os.WriteFile(f, []byte(strings.Join(out, "\n")+"\n"), 0o644)
		}
	}
}

// recentShown is the recent files that still exist, not open now.
func (m *Model) recentShown(n int) []string {
	m.loadRecent()
	var out []string
	for _, p := range m.recent {
		if len(out) == n {
			break
		}
		if _, err := os.Stat(p); err != nil || m.tabFor(p) != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// recentLines renders the start page's RECENT block and its click zones.
func (m *Model) recentLines(w, at int) []string {
	paths := m.recentShown(5)
	if len(paths) == 0 {
		return nil
	}
	out := []string{theme.SectionHeader().Render("RECENT")}
	for i, p := range paths {
		label := m.relToWorkspace(p)
		m.startZones = append(m.startZones, startZone{line: at + len(out), from: 0, to: w, key: "recent:" + itoa(i)})
		out = append(out, "  "+theme.TextStyle().Render(kit.ClipEllipsis(filepath.Base(label), 32))+
			theme.TextDim().Render("  "+kit.ClipEllipsis(filepath.Dir(label), max(w-38, 8))))
	}
	return append(out, "")
}

// openRecent opens the i-th shown recent file.
func (m *Model) openRecent(i int) {
	paths := m.recentShown(5)
	if i < len(paths) {
		m.open(&node{kind: nodeFile, path: paths[i], name: filepath.Base(paths[i])})
	}
}

// reopenClosed opens the most recently closed file again.
func (m *Model) reopenClosed() string {
	for len(m.closed) > 0 {
		p := m.closed[len(m.closed)-1]
		m.closed = m.closed[:len(m.closed)-1]
		if _, err := os.Stat(p); err == nil {
			m.open(&node{kind: nodeFile, path: p, name: filepath.Base(p)})
			return ""
		}
	}
	return "no closed file to reopen"
}
