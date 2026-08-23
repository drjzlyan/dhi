// Package home is DHI's landing dashboard: brand, orientation, and pointers
// into the rest of the IDE.
package home

import (
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Model is the workspace dashboard shown on boot.
type Model struct {
	width   int
	version string
}

var _ surfaces.Surface = (*Model)(nil)

// New returns the dashboard model.
func New(version string) *Model { return &Model{version: version} }

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "home", Title: "Home"} }
func (m *Model) Init() tea.Cmd       { return nil }
func (m *Model) Resize(w, _ int)     { m.width = w }

func (m *Model) Update(tea.Msg) tea.Cmd { return nil }
func (m *Model) HandleKey(string) bool  { return false }

func (m *Model) View() string {
	body := []string{}
	body = append(body, logoLines()...)
	body = append(body,
		"",
		theme.TextDim().Render("  the agentic workspace IDE"),
		theme.Hint().Render("  v"+m.version+"  "+theme.GlyphBullet+"  M0 foundation"),
		"",
		hintLine("1-9", "jump between workspaces"),
		hintLine("tab", "cycle workspaces"),
		hintLine("?", "toggle help"),
		hintLine(":", "command palette — soon"),
	)
	return strings.Join(body, "\n")
}

func hintLine(key, desc string) string {
	return "   " + theme.GlyphChevron + " " + theme.TabActive().Render(key) +
		"  " + theme.TextDim().Render(desc)
}

func logoLines() []string {
	rows := [6][2]string{
		{"██████╗ ", "██╗  ██╗██╗"},
		{"██╔══██╗", "██║  ██║██║"},
		{"██║  ██║", "███████║██║"},
		{"██║  ██║", "██╔══██║██║"},
		{"██████╔╝", "██║  ██║██║"},
		{"╚═════╝ ", "╚═╝  ╚═╝╚═╝"},
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		t := float64(i) / float64(len(rows)-1)
		st := lipgloss.NewStyle().
			Foreground(theme.Blend(theme.Current.Accent, theme.Current.Accent2, t)).
			Bold(true)
		out[i] = " " + st.Render(r[0]+r[1])
	}
	return out
}
