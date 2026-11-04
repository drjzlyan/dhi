package editor

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Pane limits. A split is refused (and a restored or resized layout folds
// back to one pane) when any pane would be smaller than this.
const (
	splitMinPaneW = 36 // columns, panel edges included (F-057)
	splitMinPaneH = 8  // rows: edges, tab strip and a few lines of text (F-166)
	maxPanes      = 4  // a layout tree holds at most this many panes (F-166)
)

// Split panes (F-057, F-166): the buffer area is a small tree of panes.
// A node either shows one tab (a leaf) or divides its box among its kids,
// side by side (paneCols) or stacked (paneRows); a split inside a node of
// the same direction adds a sibling instead of nesting. The focused pane
// is the one whose tab is the active tab; every other pane keeps its own
// tab. Two panes may show the same tab: they then share the buffer's one
// cursor and scroll (a buffer has a single editor), which is the limit of
// the one-editor-per-tab model.
//
// Keys use vim's window prefix in normal mode: ctrl+w v / s split, h j k l
// move focus by direction, w cycles, c / q close the pane, o keeps only
// it, = equalizes. The ex commands :vs, :sp, :only and :close do the same.

type paneDir int

const (
	paneCols paneDir = iota // kids side by side
	paneRows                // kids stacked
)

// pane is a layout node: a leaf when kids is empty.
type pane struct {
	tab  *bufTab
	dir  paneDir
	kids []*pane
}

func (p *pane) leaf() bool { return len(p.kids) == 0 }

// leaves lists the panes in reading order.
func (p *pane) leaves() []*pane {
	if p == nil {
		return nil
	}
	if p.leaf() {
		return []*pane{p}
	}
	var out []*pane
	for _, k := range p.kids {
		out = append(out, k.leaves()...)
	}
	return out
}

// parentOf finds n's parent in the tree under p (nil for the root).
func (p *pane) parentOf(n *pane) *pane {
	for _, k := range p.kids {
		if k == n {
			return p
		}
		if q := k.parentOf(n); q != nil {
			return q
		}
	}
	return nil
}

// paneRect is a leaf's box on the surface, recorded by the layout and the
// last render; geom is where its text sits, for clicks.
type paneRect struct {
	leaf       *pane
	x, y, w, h int
	geom       bufGeom
}

// span divides total among n parts; the last takes the remainder.
func span(total, n, i int) (off, size int) {
	each := total / n
	off = each * i
	size = each
	if i == n-1 {
		size = total - off
	}
	return off, size
}

// layoutRects walks the tree, placing each leaf in its share of the box.
func layoutRects(p *pane, x, y, w, h int, out *[]paneRect) {
	if p.leaf() {
		*out = append(*out, paneRect{leaf: p, x: x, y: y, w: w, h: h})
		return
	}
	for i, k := range p.kids {
		if p.dir == paneCols {
			off, sz := span(w, len(p.kids), i)
			layoutRects(k, x+off, y, sz, h, out)
		} else {
			off, sz := span(h, len(p.kids), i)
			layoutRects(k, x, y+off, w, sz, out)
		}
	}
}

func rectsFit(rs []paneRect) bool {
	for _, r := range rs {
		if r.w < splitMinPaneW || r.h < splitMinPaneH {
			return false
		}
	}
	return true
}

// bodyHeight is the rows the buffer area gets once the bottom panels are
// stacked under it.
func (m *Model) bodyHeight() int {
	bodyH := m.height
	if m.drawerOpen {
		bodyH = m.height - min(drawerHeight, maxInt(m.height/3, 4)) - 1 // drawer + its hint footer
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
	return bodyH
}

// mainBox is the buffer area's surface box (the rail on its left and the
// crew panel on its right excluded).
func (m *Model) mainBox() (x, w, h int) {
	x = m.railW()
	w = m.width - x
	if m.chat != nil && m.chat.open {
		w -= chatWidth
	}
	return x, maxInt(w, 10), m.bodyHeight()
}

// paneRects is the current layout in surface cells; nil with one pane.
func (m *Model) paneRects() []paneRect {
	if !m.splitActive() {
		return nil
	}
	x, w, h := m.mainBox()
	var rs []paneRect
	layoutRects(m.layout, x, 0, w, h, &rs)
	return rs
}

// splitActive reports a shown layout of two or more panes. It also keeps
// the tree honest: a pane whose tab was closed goes away, and the focused
// pane always shows the active tab.
func (m *Model) splitActive() bool {
	if m.layout == nil {
		return false
	}
	for _, l := range m.layout.leaves() {
		if m.layout == nil {
			return false
		}
		if m.indexOfTab(l.tab) < 0 {
			m.removePane(l)
		}
	}
	if m.layout == nil || m.focus == nil {
		return false
	}
	if m.activeTab >= 0 && m.activeTab < len(m.bufs) {
		m.focus.tab = m.bufs[m.activeTab]
	}
	return m.active() != nil
}

// removePane takes a leaf out of the tree. When the focused pane goes,
// focus moves to the nearest remaining pane; one pane left ends the split.
func (m *Model) removePane(l *pane) {
	at := 0
	for i, p := range m.layout.leaves() {
		if p == l {
			at = i
		}
	}
	parent := m.layout.parentOf(l)
	if parent == nil { // the root was the only leaf
		m.layout, m.focus = nil, nil
		return
	}
	for i, k := range parent.kids {
		if k == l {
			parent.kids = append(parent.kids[:i:i], parent.kids[i+1:]...)
			break
		}
	}
	if len(parent.kids) == 1 {
		only := parent.kids[0]
		if gp := m.layout.parentOf(parent); gp == nil {
			m.layout = only
		} else {
			for i, k := range gp.kids {
				if k != parent {
					continue
				}
				if !only.leaf() && only.dir == gp.dir { // same direction: splice, never nest
					rest := append([]*pane{}, gp.kids[i+1:]...)
					gp.kids = append(append(gp.kids[:i:i], only.kids...), rest...)
				} else {
					gp.kids[i] = only
				}
				break
			}
		}
	}
	rest := m.layout.leaves()
	if len(rest) <= 1 {
		if len(rest) == 1 && m.focus == l {
			m.focusIndex(rest[0])
		}
		m.layout, m.focus = nil, nil
		return
	}
	if m.focus == l {
		m.focusPane(rest[min(at, len(rest)-1)])
	}
}

// focusIndex makes the active tab the pane's tab.
func (m *Model) focusIndex(l *pane) {
	if i := m.indexOfTab(l.tab); i >= 0 {
		m.activeTab = i
	}
}

// focusPane moves focus to a pane, which makes its tab the active one.
func (m *Model) focusPane(l *pane) {
	m.focus = l
	m.focusIndex(l)
}

// splitWith divides the focused pane in the given direction, the new pane
// showing the previous tab that no pane shows yet (else the previous tab,
// else the same buffer, like vim's :split). Focus stays where it was. It
// returns the refusal, or "" when the split happened.
func (m *Model) splitWith(dir paneDir) string {
	m.splitActive()
	if m.active() == nil {
		return "nothing to split"
	}
	if m.layout == nil {
		cur := &pane{tab: m.bufs[m.activeTab]}
		m.layout, m.focus = cur, cur
	}
	if len(m.layout.leaves()) >= maxPanes {
		return "at most " + itoa(maxPanes) + " panes"
	}
	shown := map[*bufTab]bool{}
	for _, l := range m.layout.leaves() {
		shown[l.tab] = true
	}
	tab := m.bufs[m.activeTab]
	if n := len(m.bufs); n > 1 {
		tab = m.bufs[(m.activeTab-1+n)%n]
		for k := 1; k < n; k++ {
			if c := m.bufs[(m.activeTab-k+n)%n]; !shown[c] {
				tab = c
				break
			}
		}
	}
	added := &pane{tab: tab}
	if parent := m.layout.parentOf(m.focus); parent != nil && parent.dir == dir {
		for i, k := range parent.kids {
			if k == m.focus {
				parent.kids = append(parent.kids[:i+1:i+1], append([]*pane{added}, parent.kids[i+1:]...)...)
				break
			}
		}
	} else {
		node := &pane{dir: dir, kids: []*pane{m.focus, added}}
		if parent == nil {
			m.layout = node
		} else {
			for i, k := range parent.kids {
				if k == m.focus {
					parent.kids[i] = node
				}
			}
		}
	}
	if m.width > 0 {
		x, w, h := m.mainBox()
		var rs []paneRect
		layoutRects(m.layout, x, 0, w, h, &rs)
		if !rectsFit(rs) {
			f := m.focus
			m.removePane(added)
			if m.layout != nil {
				m.focus = f
			}
			return "no room to split · the window is too small"
		}
	}
	return "split · ctrl+w w cycles panes"
}

// openSplit splits side by side (:vsplit).
func (m *Model) openSplit() { m.splitWith(paneCols) }

// closePane closes the focused pane (its tab stays open).
func (m *Model) closePane() string {
	if !m.splitActive() {
		return "one pane"
	}
	m.removePane(m.focus)
	return "pane closed"
}

// onlyPane returns to a single pane (every tab stays open).
func (m *Model) onlyPane() string {
	m.layout, m.focus = nil, nil
	return "one pane"
}

// cyclePane moves focus to the next (or previous) pane in reading order.
func (m *Model) cyclePane(step int) {
	if !m.splitActive() {
		return
	}
	all := m.layout.leaves()
	for i, l := range all {
		if l == m.focus {
			m.focusPane(all[(i+step+len(all))%len(all)])
			return
		}
	}
}

// focusDirection moves focus to the nearest pane in a direction (h j k l).
func (m *Model) focusDirection(dx, dy int) {
	rs := m.paneRects()
	var cur *paneRect
	for i := range rs {
		if rs[i].leaf == m.focus {
			cur = &rs[i]
		}
	}
	if cur == nil {
		return
	}
	var best *paneRect
	bestGap, bestOff := 0, 0
	for i := range rs {
		o := &rs[i]
		if o == cur {
			continue
		}
		var gap, off int
		switch {
		case dx > 0 && o.x >= cur.x+cur.w && overlaps(o.y, o.h, cur.y, cur.h):
			gap, off = o.x-(cur.x+cur.w), absInt(o.y-cur.y)
		case dx < 0 && o.x+o.w <= cur.x && overlaps(o.y, o.h, cur.y, cur.h):
			gap, off = cur.x-(o.x+o.w), absInt(o.y-cur.y)
		case dy > 0 && o.y >= cur.y+cur.h && overlaps(o.x, o.w, cur.x, cur.w):
			gap, off = o.y-(cur.y+cur.h), absInt(o.x-cur.x)
		case dy < 0 && o.y+o.h <= cur.y && overlaps(o.x, o.w, cur.x, cur.w):
			gap, off = cur.y-(o.y+o.h), absInt(o.x-cur.x)
		default:
			continue
		}
		if best == nil || gap < bestGap || (gap == bestGap && off < bestOff) {
			best, bestGap, bestOff = o, gap, off
		}
	}
	if best != nil {
		m.focusPane(best.leaf)
	}
}

func overlaps(a, al, b, bl int) bool { return a < b+bl && b < a+al }

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// splitKey handles the key after the ctrl+w prefix.
func (m *Model) splitKey(key string) {
	var msg string
	switch key {
	case "v", "ctrl+v":
		msg = m.splitWith(paneCols)
	case "s", "ctrl+s":
		msg = m.splitWith(paneRows)
	case "w", "ctrl+w":
		m.cyclePane(1)
	case "W":
		m.cyclePane(-1)
	case "h", "left":
		m.focusDirection(-1, 0)
	case "l", "right":
		m.focusDirection(1, 0)
	case "k", "up":
		m.focusDirection(0, -1)
	case "j", "down":
		m.focusDirection(0, 1)
	case "q", "c":
		m.closePane()
	case "o", "ctrl+o":
		m.onlyPane()
	case "=":
		// Shares are always derived from the tree (there are no resize
		// keys), so panes are already equal.
	}
	if strings.HasPrefix(msg, "no room") || strings.HasPrefix(msg, "at most") {
		m.toast = &kit.Toast{Kind: kit.ToastInfo, Title: "Split", Detail: msg}
	}
}

// splitPanels renders the layout into mainW × bodyH, the focused pane
// with the identity accent and the others dimmed-edged. It returns ""
// when a pane would be too small (the caller shows one pane).
func (m *Model) splitPanels(mainW, bodyH int) string {
	x := m.railW()
	var rs []paneRect
	layoutRects(m.layout, x, 0, mainW, bodyH, &rs)
	if !rectsFit(rs) {
		m.panes = nil
		return ""
	}
	save := m.activeTab
	m.panes = m.panes[:0]
	out := m.renderPane(m.layout, x, 0, mainW, bodyH)
	m.activeTab = save
	m.renderingSplit, m.paneW, m.paneH = false, 0, 0
	for _, r := range m.panes {
		if r.leaf == m.focus {
			m.bufGeom = r.geom
		}
	}
	return out
}

// renderPane draws one node in its box and records each leaf's rect.
func (m *Model) renderPane(p *pane, x, y, w, h int) string {
	if p.leaf() {
		i := m.indexOfTab(p.tab)
		m.activeTab = i
		focused := p == m.focus
		m.renderingSplit = !focused // popups belong to the focused pane
		m.paneW, m.paneH = w, h
		out := m.bufferPanel(i, w, h, focused)
		g := m.bufGeom
		g.ok = true
		g.ox, g.oy = x+1, y+1+len(splitLines(tabStrip(m.bufs, i, w-2)))
		m.panes = append(m.panes, paneRect{leaf: p, x: x, y: y, w: w, h: h, geom: g})
		return out
	}
	parts := make([]string, len(p.kids))
	for i, k := range p.kids {
		if p.dir == paneCols {
			off, sz := span(w, len(p.kids), i)
			parts[i] = m.renderPane(k, x+off, y, sz, h)
		} else {
			off, sz := span(h, len(p.kids), i)
			parts[i] = m.renderPane(k, x, y+off, w, sz)
		}
	}
	if p.dir == paneRows {
		return joinV(parts...)
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out = joinH(out, part)
	}
	return out
}

// paneAt finds the pane under a surface cell from the last render.
func (m *Model) paneAt(x, y int) *paneRect {
	for i := range m.panes {
		r := &m.panes[i]
		if x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h {
			return r
		}
	}
	return nil
}

// bufferPanel renders tab i's buffer view in a w×h panel.
func (m *Model) bufferPanel(i, w, h int, focused bool) string {
	e := m.bufs[i].ed
	var title string
	if focused {
		chips := m.diagChip(e)
		title = m.bufferTitle(e, m.bufs[i].vp, w, chips) + chips
	} else {
		title = kit.Restyle(theme.TextDim(), m.bufferTitle(e, m.bufs[i].vp, w, "")) // the mode pill inside keeps its own colors
	}
	body := joinV(tabStrip(m.bufs, i, w-2), m.bufferView())
	p := kit.NewPanel(title, focused)
	p.SetContent(splitLines(body)...)
	p.Width, p.Height = w, h
	return p.View()
}

// splitCommand handles :vs, :sp, :only and :close (nil-safe; false = not ours).
func (m *Model) splitCommand(cmd string) (string, bool) {
	switch strings.TrimSpace(cmd) {
	case "vs", "vsplit", "vsp":
		return m.splitWith(paneCols), true
	case "sp", "split":
		return m.splitWith(paneRows), true
	case "only", "on":
		return m.onlyPane(), true
	case "clo", "close":
		return m.closePane(), true
	}
	return "", false
}
