// Package repairgate is the launch card for self-healing (F-085,
// ADR-0038): it says what DHI fixed on its own and asks before fixing
// anything the team tracks. It shows only when there is something to say.
package repairgate

import (
	tea "charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/repair"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Model is the gate.
type Model struct {
	version       string
	fixed         repair.Result // safe repairs already applied at launch
	ask           []repair.Repair
	after         *repair.Result // set once the asked repairs ran
	width, height int
	finished      bool
}

// New builds the gate; Needed reports whether it has anything to show.
func New(version string, fixed repair.Result, ask []repair.Repair) *Model {
	return &Model{version: version, fixed: fixed, ask: ask}
}

// Needed is true when something was fixed, failed, or awaits a yes.
func (m *Model) Needed() bool {
	return len(m.fixed.Done)+len(m.fixed.Failed)+len(m.ask) > 0
}

func (m *Model) Init() tea.Cmd            { return nil }
func (m *Model) Update(tea.Msg) tea.Cmd   { return nil }
func (m *Model) Finished() bool           { return m.finished }
func (m *Model) Resize(width, height int) { m.width, m.height = width, height }
func (m *Model) CapturesInput() bool      { return false }
func (m *Model) asking() bool             { return len(m.ask) > 0 && m.after == nil }

// HandleKey: enter fixes what was asked (or continues); esc / n leaves
// it for later — doctor and this card name it again next launch.
func (m *Model) HandleKey(key string) bool {
	switch {
	case m.asking() && (key == "enter" || key == "y"):
		res := repair.Run(m.ask, false)
		m.after = &res
	case m.asking() && (key == "esc" || key == "n"):
		m.finished = true
	case key == "enter" || key == "esc" || key == " ":
		m.finished = true
	}
	return true
}

func (m *Model) View() string {
	w := min(max(m.width-10, 40), 84)
	var lines []string
	push := func(style func(string) string, s string) {
		for _, l := range kit.WrapWords(s, w-4) {
			lines = append(lines, style(l))
		}
	}
	plain := func(s string) string { return theme.TextStyle().Render(s) }
	dim := func(s string) string { return theme.TextDim().Render(s) }
	good := func(s string) string { return theme.SuccessText().Render(s) }
	bad := func(s string) string { return theme.DangerText().Render(s) }

	push(func(s string) string { return theme.Brand().Render(s) }, "DHI checked your workspace")
	lines = append(lines, "")
	list := func(title string, items []string, mark string, style func(string) string) {
		if len(items) == 0 {
			return
		}
		push(plain, title)
		for _, it := range items {
			push(style, "  "+mark+" "+it)
		}
		lines = append(lines, "")
	}
	list("Fixed on its own:", m.fixed.Done, theme.GlyphCheck, good)
	list("Could not fix:", m.fixed.Failed, theme.GlyphCross, bad)
	switch {
	case m.asking():
		var items []string
		for _, r := range m.ask {
			items = append(items, r.What)
		}
		list("Can fix, with your OK (these change files your team shares):", items, "•", plain)
		lines = append(lines, theme.Hint().Render("enter fix them · esc not now"))
	case m.after != nil:
		list("Fixed:", m.after.Done, theme.GlyphCheck, good)
		list("Could not fix:", m.after.Failed, theme.GlyphCross, bad)
		lines = append(lines, theme.Hint().Render("enter continue"))
	default:
		push(dim, "Nothing else needs you.")
		lines = append(lines, "", theme.Hint().Render("enter continue"))
	}
	p := kit.NewPanel("dhi "+m.version, false)
	p.Boxed = true // gates keep their frame (ADR-0040)
	p.SetContent(lines...)
	p.Width = w
	p.Height = min(len(lines)+2, max(m.height, 8))
	return kit.Center(p.View(), max(m.width, p.Width), max(m.height, p.Height))
}
