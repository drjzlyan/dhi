// Package placeholder provides the standard "planned milestone" surface used
// until each feature lands. It keeps the shell fully navigable from day one
// while making scope explicit in the UI itself.
package placeholder

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/version"
)

// Model renders a panel describing a future surface.
type Model struct {
	id, title string
	milestone string
	summary   string
	version   string

	width, height int
}

var _ surfaces.Surface = (*Model)(nil)

// New builds a placeholder surface.
func New(id, title, milestone, summary string) *Model {
	return &Model{id: id, title: title, milestone: milestone, summary: summary, version: version.Version}
}

func (m *Model) Meta() surfaces.Meta    { return surfaces.Meta{ID: m.id, Title: m.title} }
func (m *Model) Init() tea.Cmd          { return nil }
func (m *Model) Resize(w, h int)        { m.width, m.height = w, h }
func (m *Model) Update(tea.Msg) tea.Cmd { return nil }

func (m *Model) HandleKey(string) bool { return false }

// View composes the brand hero with the surface's identity — the same
// hero-first pattern as the bootstrap gate, so every not-yet-built
// screen feels intentional rather than empty.
func (m *Model) View() string {
	body := []string{}

	if m.height >= 18 {
		body = append(body, strings.Split(branding.HeroBlock(m.version), "\n")...)
		body = append(body, "")
	}

	body = append(body,
		theme.Brand().Render(m.title),
		theme.TextDim().Render(m.summary),
		"",
		theme.WarningText().Render("PLANNED")+theme.Hint().Render("  lands in "+m.milestone),
	)

	if m.height >= 10 {
		body = append(body, "", theme.Hint().Render(
			"this surface ships in a later milestone — nothing to configure yet"))
	}
	return kit.Center(strings.Join(body, "\n"), maxInt(m.width, 40), maxInt(m.height, 10))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
