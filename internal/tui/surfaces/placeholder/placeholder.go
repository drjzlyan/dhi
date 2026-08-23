// Package placeholder provides the standard "planned milestone" surface used
// until each feature lands. It keeps the shell fully navigable from day one
// while making scope explicit in the UI itself.
package placeholder

import (
	"strings"

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
	panel         *kit.Panel
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

func (m *Model) View() string {
	w := m.width - 2
	if w < 40 {
		w = 40
	}
	h := m.height - 4
	if h < 8 {
		h = 8
	}
	p := kit.NewPanel(m.title, true).SetContent(
		"",
		"  "+theme.WarningText().Render("PLANNED")+theme.Hint().Render(" lands in "+m.milestone),
		"",
		"  "+theme.TextDim().Render(m.summary),
	)
	p.Width, p.Height = w, h
	return strings.Join([]string{"", p.View()}, "\n")
}
