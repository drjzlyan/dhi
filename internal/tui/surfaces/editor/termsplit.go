package editor

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// termSplitMinPaneW is the narrowest a drawer pane may be; below it the
// split folds back to the focused pane (both tabs stay open), like the
// buffer split (F-057).
const termSplitMinPaneW = 20

// Split terminals (F-141): the drawer shows two tabs side by side. The
// pair is remembered by tab id (termSplit); the focused pane is the
// active tab. alt+s splits, alt+o moves focus, alt+w closes one pane.

// termSplitActive reports a shown split: both tabs alive, the active tab
// one of them, and the drawer wide enough for two panes.
func (m *Model) termSplitActive() bool {
	a, b := m.termIndex(m.termSplit[0]), m.termIndex(m.termSplit[1])
	if m.termSplit[0] == 0 || a < 0 || b < 0 || (m.activeTerm != a && m.activeTerm != b) {
		return false
	}
	l, r := m.paneWidths()
	return l >= termSplitMinPaneW && r >= termSplitMinPaneW
}

// paneWidths splits the drawer body into two panes and a divider column.
func (m *Model) paneWidths() (left, right int) {
	avail := maxInt(m.width-4, 16)
	left = (avail - 1) / 2
	return left, avail - 1 - left
}

// paneWidth is the cell width of the pane showing tab i in a shown split,
// or 0 when the tab is not part of one.
func (m *Model) paneWidth(i int) int {
	if !m.termSplitActive() {
		return 0
	}
	l, r := m.paneWidths()
	switch i {
	case m.termIndex(m.termSplit[0]):
		return l
	case m.termIndex(m.termSplit[1]):
		return r
	}
	return 0
}

// splitTerm opens a new shell in the active tab's folder beside it.
func (m *Model) splitTerm() {
	t := m.activeTermTab()
	if t == nil {
		return
	}
	if m.termSplitActive() {
		m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Terminal", Detail: "already split · close a pane with alt+w"}
		return
	}
	if l, r := m.paneWidths(); l < termSplitMinPaneW || r < termSplitMinPaneW {
		m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Terminal", Detail: "too narrow to split"}
		return
	}
	dir, label := m.activeTermDir()
	if dir == "" {
		return
	}
	cur := t.id
	m.newTermTab(dir, label+"-"+itoa(len(m.terms)+1))
	m.activeTerm = len(m.terms) - 1
	m.termSplit = [2]int{cur, m.terms[m.activeTerm].id}
	m.syncTermSize()
}

// focusOtherPane moves keyboard focus to the other pane of a split.
func (m *Model) focusOtherPane() {
	if !m.termSplitActive() {
		return
	}
	for _, id := range m.termSplit {
		if i := m.termIndex(id); i != m.activeTerm {
			m.activeTerm = i
			break
		}
	}
	m.syncTermSize()
}

// closeTermPane closes the focused tab; in a split the other pane becomes
// the single pane.
func (m *Model) closeTermPane() {
	other := 0
	if m.termSplitActive() {
		for _, id := range m.termSplit {
			if id != m.terms[m.activeTerm].id {
				other = id
			}
		}
	}
	m.closeTerm(m.activeTerm)
	m.termSplit = [2]int{}
	if i := m.termIndex(other); i >= 0 {
		m.activeTerm = i
	}
	m.syncTermSize()
}

// splitBody joins the two panes' rows around a divider.
func (m *Model) splitBody(rows int) []string {
	lw, rw := m.paneWidths()
	l := m.termIndex(m.termSplit[0])
	r := m.termIndex(m.termSplit[1])
	left := m.termBodyOf(m.terms[l], lw, rows, m.termFocus && m.activeTerm == l)
	right := m.termBodyOf(m.terms[r], rw, rows, m.termFocus && m.activeTerm == r)
	div := theme.DeckDivider(m.termFocus).Render(theme.GlyphDivider)
	out := make([]string, rows)
	for i := range out {
		out[i] = padCells(rowAt(left, i), lw) + div + xansi.Truncate(rowAt(right, i), rw, "")
	}
	return out
}

func rowAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// padCells clips s to w cells and pads it with spaces to exactly w.
func padCells(s string, w int) string {
	s = xansi.Truncate(s, w, "")
	if n := xansi.StringWidth(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}
