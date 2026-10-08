package editor

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// splitMinPaneW is the narrowest a split pane may be; below it the
// split folds back to one pane (the other buffer stays open as a tab).
const splitMinPaneW = 36

// Split panes (F-057, the M23 deferral): two buffers side by side. The
// focused pane is the active tab; splitTab is the other one. Keys use
// vim's window prefix in normal mode — ctrl+w v splits, ctrl+w w (or h/l)
// switches panes, ctrl+w q / ctrl+w o closes the split — and the ex
// commands :vs and :only do the same.

// openSplit shows a second pane: the previous buffer when there is one,
// else the same buffer twice (like vim's :vsplit).
func (m *Model) openSplit() string {
	if m.active() == nil {
		return "nothing to split"
	}
	m.splitTab = m.activeTab
	if len(m.bufs) > 1 {
		m.splitTab = (m.activeTab - 1 + len(m.bufs)) % len(m.bufs)
	}
	m.split = true
	return "split · ctrl+w w switches panes"
}

// closeSplit returns to a single pane (both buffers stay open as tabs).
func (m *Model) closeSplit() string {
	m.split = false
	return "one pane"
}

// swapSplit moves focus to the other pane.
func (m *Model) swapSplit() {
	if m.splitActive() {
		m.activeTab, m.splitTab = m.splitTab, m.activeTab
	}
}

// splitActive reports a valid, shown split (closing a buffer can leave
// the remembered tab out of range: the split then quietly ends).
func (m *Model) splitActive() bool {
	if m.split && (m.splitTab < 0 || m.splitTab >= len(m.bufs)) {
		m.split = false
	}
	return m.split && m.active() != nil
}

// splitKey handles the key after the ctrl+w prefix.
func (m *Model) splitKey(key string) {
	switch key {
	case "v", "ctrl+v", "s":
		m.openSplit()
	case "w", "ctrl+w", "h", "l", "left", "right":
		m.swapSplit()
	case "q", "c", "o", "ctrl+o":
		m.closeSplit()
	}
}

// splitPanels renders the two panes into mainW × bodyH: the focused
// pane on the left keeps the identity accent, the other is dimmed-edged.
// It returns "" when the window is too narrow (the caller shows one pane).
func (m *Model) splitPanels(mainW, bodyH int) string {
	leftW := mainW / 2
	rightW := mainW - leftW
	if leftW < splitMinPaneW || rightW < splitMinPaneW {
		return ""
	}
	focused := m.bufferPanel(m.activeTab, leftW, bodyH, true)
	save := m.activeTab
	m.activeTab = m.splitTab
	m.renderingSplit = true
	other := m.bufferPanel(m.activeTab, rightW, bodyH, false)
	m.renderingSplit = false
	m.activeTab = save
	return joinH(focused, other)
}

// bufferPanel renders tab i's buffer view in a w×h panel.
func (m *Model) bufferPanel(i, w, h int, focused bool) string {
	e := m.bufs[i].ed
	title := bufferTitle(e)
	if focused {
		title += m.diagChip(e)
	} else {
		title = theme.TextDim().Render(title)
	}
	body := joinV(tabStrip(m.bufs, i, w-2), m.bufferView())
	p := kit.NewPanel(title, focused)
	p.SetContent(splitLines(body)...)
	p.Width, p.Height = w, h
	return p.View()
}

// splitCommand handles :vs/:vsplit and :only (nil-safe; false = not ours).
func (m *Model) splitCommand(cmd string) (string, bool) {
	switch strings.TrimSpace(cmd) {
	case "vs", "vsplit", "vsp":
		return m.openSplit(), true
	case "only", "on", "clo", "close":
		return m.closeSplit(), true
	}
	return "", false
}
