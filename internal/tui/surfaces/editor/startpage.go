package editor

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// StartDeps wires the start page's actions that live outside the editor
// (F-097): the setup checklist, running one setup step, the tour and a
// new task. Every field is optional; a nil one hides its action.
type StartDeps struct {
	// Checklist lists the setup steps that apply; all settled (or nil)
	// hides the card.
	Checklist func() []kit.ChecklistItem
	RunStep   func(id string)
	StartTour func()
	NewTask   func()
}

// WithStart wires the start page (F-097).
func WithStart(d StartDeps) Option { return func(m *Model) { m.start = d } }

// startZone is a clickable span of the start page, in page coordinates.
type startZone struct {
	line, from, to int
	key            string // a key the page handles, or "step:<id>"
}

// startMaxW keeps the page readable on wide terminals.
const startMaxW = 76

// startVisible reports whether the main pane shows the start page:
// nothing open, no search results, and room beside the tree.
func (m *Model) startVisible() bool {
	if m.ws == nil {
		return m.start.Checklist != nil
	}
	return m.mode == modeNav && m.active() == nil && m.openPath == "" && m.railW() < m.width
}

// pendingSteps returns the checklist and the first pending item's id.
func (m *Model) pendingSteps() ([]kit.ChecklistItem, string) {
	if m.start.Checklist == nil {
		return nil, ""
	}
	items := m.start.Checklist()
	for _, it := range items {
		if it.State == kit.ItemPending {
			return items, it.ID
		}
	}
	return items, ""
}

// startPage renders the page w cells wide and records its click zones.
func (m *Model) startPage(w int) []string {
	w = min(w, startMaxW)
	m.startZones = m.startZones[:0]
	var out []string
	name := ""
	if m.ws != nil {
		name = filepath.Base(m.ws.Root)
	}
	if m.ws != nil { // outside a workspace the hero above says it
		out = append(out,
			theme.Brand().Render("◆ dhi")+theme.TextDim().Render("  ·  "+name),
			theme.TextMuted().Render(branding.Tagline()),
			"")
	}

	var bs []kit.Button
	if m.ws != nil {
		bs = append(bs, kit.Button{Key: "/", Label: "Find a file", Focused: true})
		if m.searcher != nil && len(m.members) > 0 {
			bs = append(bs, kit.Button{Key: "s", Label: "Search code", Variant: theme.ButtonSecondary})
		}
		bs = append(bs, kit.Button{Key: "^t", Label: "Terminal", Variant: theme.ButtonSecondary})
		if m.chat != nil {
			bs = append(bs, kit.Button{Key: "^a", Label: "Ask an agent", Variant: theme.ButtonSecondary})
		}
		if m.start.NewTask != nil {
			bs = append(bs, kit.Button{Key: "n", Label: "New task", Variant: theme.ButtonSecondary})
		}
	}
	if len(bs) > 0 {
		lines, spans := kit.Buttons(bs, w)
		for _, s := range spans {
			key := s.Key
			switch key {
			case "^t":
				key = "ctrl+t"
			case "^a":
				key = "ctrl+a"
			}
			m.startZones = append(m.startZones, startZone{line: len(out) + s.Line, from: s.From, to: s.To, key: key})
		}
		out = append(out, lines...)
		out = append(out, "")
	}

	items, next := m.pendingSteps()
	// Outside a workspace the card always shows: its workspace row is the
	// only way in, even after everything was skipped.
	if next != "" || (m.ws == nil && len(items) > 0) {
		cl := kit.Checklist{Items: items, Focus: next != "", ActionKey: "c"}
		for i, it := range items {
			if it.ID == next {
				cl.Cursor = i
			}
		}
		out = append(out, theme.SectionHeader().Render("GET SET UP")+theme.TextMuted().Render("  "+progressLine(cl)))
		for i, row := range cl.View(w) {
			m.startZones = append(m.startZones, startZone{line: len(out), from: 0, to: w, key: "step:" + items[i].ID})
			out = append(out, row)
		}
		out = append(out, "")
	}

	tips := []string{"? help", "^p palette"}
	if m.start.StartTour != nil {
		tips = append(tips, "t take the tour")
	}
	if m.ws != nil {
		tips = append(tips, kit.AltLabel("alt+1-5 views"))
	}
	out = append(out, theme.Hint().Render(kit.ClipEllipsis(strings.Join(tips, "  ·  "), w)))
	return out
}

// progressLine says where setup stands: "1 done · 1 skipped · 7 to go",
// each part only when it is non-zero, and "about a minute each" while
// nothing has been done yet.
func progressLine(cl kit.Checklist) string {
	done, settled := cl.Done(), cl.Settled()
	skipped, left := settled-done, len(cl.Items)-settled
	var parts []string
	if done > 0 {
		parts = append(parts, strconv.Itoa(done)+" done")
	}
	if skipped > 0 {
		parts = append(parts, strconv.Itoa(skipped)+" skipped")
	}
	parts = append(parts, strconv.Itoa(left)+" to go")
	if settled == 0 {
		parts = append(parts, "about a minute each")
	}
	return strings.Join(parts, " · ")
}

// startKey handles the start page's own keys; false leaves the key to
// the tree.
func (m *Model) startKey(key string) bool {
	switch key {
	case "c":
		if _, next := m.pendingSteps(); next != "" && m.start.RunStep != nil {
			m.start.RunStep(next)
			return true
		}
	case "t":
		if m.start.StartTour != nil {
			m.start.StartTour()
			return true
		}
	case "n":
		if m.start.NewTask != nil && m.ws != nil {
			m.start.NewTask()
			return true
		}
	}
	return false
}

// startClick resolves a click at page coordinates (x, y).
func (m *Model) startClick(x, y int) bool {
	for _, z := range m.startZones {
		if y != z.line || x < z.from || x >= z.to {
			continue
		}
		if id, ok := strings.CutPrefix(z.key, "step:"); ok {
			if m.start.RunStep != nil {
				m.start.RunStep(id)
			}
			return true
		}
		m.HandleKey(z.key)
		return true
	}
	return false
}

// noWorkspaceStart is the whole-screen start page outside a workspace:
// the hero, then the checklist whose first item creates one.
func (m *Model) noWorkspaceStart() string {
	page := m.startPage(m.width - 4)
	hero := []string{}
	if m.width >= 40 && m.height >= len(page)+8 {
		hero = append(strings.Split(branding.HeroBlock(m.version), "\n"), "")
	}
	lines := append(hero, page...)
	top, left := m.centerOffsets(lines, m.width, m.height)
	m.startOX, m.startOY = left, top+len(hero)
	return kit.Center(strings.Join(lines, "\n"), m.width, m.height)
}

// centerOffsets mirrors kit.Center's placement of block in w×h so click
// zones line up with what is drawn.
func (m *Model) centerOffsets(block []string, w, h int) (top, left int) {
	bw := 0
	for _, l := range block {
		bw = max(bw, ansi.Width(l))
	}
	return max((h-len(block))/2, 0), max((w-min(bw, w))/2, 0)
}
