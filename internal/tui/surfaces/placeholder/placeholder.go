// Package placeholder provides the standard "planned milestone" surface used
// until each feature lands. It keeps the shell fully navigable from day one
// while making scope explicit in the UI itself.
package placeholder

import (
	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Model renders a panel describing a future surface.
type Model struct {
	id, title string
	milestone string
	summary   string

	width, height int
}

var _ surfaces.Surface = (*Model)(nil)

// New builds a placeholder surface.
func New(id, title, milestone, summary string) *Model {
	return &Model{id: id, title: title, milestone: milestone, summary: summary}
}

func (m *Model) Meta() surfaces.Meta    { return surfaces.Meta{ID: m.id, Title: m.title} }
func (m *Model) Init() tea.Cmd          { return nil }
func (m *Model) Resize(w, h int)        { m.width, m.height = w, h }
func (m *Model) Update(tea.Msg) tea.Cmd { return nil }

func (m *Model) HandleKey(string) bool { return false }

// View renders the docked placeholder panel: identity at the top,
// milestone chip pinned to the foot — consistent with the other
// full-height surfaces. (The brand hero belongs to the bootstrap gate
// and the not-inside-a-workspace state.)
func (m *Model) View() string {
	w := maxInt(m.width, 40)
	h := maxInt(m.height, 10)

	content := []string{
		theme.Brand().Render(m.title),
		"",
		theme.TextDim().Render(m.summary),
	}
	for len(content) < h-4 {
		content = append(content, "")
	}
	content = append(content,
		theme.WarningText().Render("PLANNED")+theme.Hint().Render("  lands in "+m.milestone),
	)

	p := kit.NewPanel(m.title, true)
	p.SetContent(content...)
	p.Width, p.Height = w, h
	return p.View()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
