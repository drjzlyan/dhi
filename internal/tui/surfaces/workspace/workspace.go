// Package workspace is DHI's landing view: the company of agents — channels,
// organization, tasks, and inspection. It boots first and carries the brand
// hero until M4 fills it with live content.
package workspace

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/surfaces"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// Model is the Workspace landing surface.
type Model struct {
	version       string
	width, height int
}

var _ surfaces.Surface = (*Model)(nil)

// New returns the workspace model.
func New(version string) *Model { return &Model{version: version} }

func (m *Model) Meta() surfaces.Meta { return surfaces.Meta{ID: "workspace", Title: "Workspace"} }
func (m *Model) Init() tea.Cmd       { return nil }
func (m *Model) Resize(w, h int)     { m.width, m.height = w, h }

func (m *Model) Update(tea.Msg) tea.Cmd { return nil }
func (m *Model) HandleKey(string) bool  { return false }

func (m *Model) View() string {
	caps := []string{
		capLine("#general", "team channels & threads", "M4"),
		capLine("org", "form teams of agents", "M4"),
		capLine("tasks", "assign · watch · review", "M4"),
		capLine("inspection", "memory · knowledge · activity", "M4"),
		capLine("marketplace", "install agent packs", "M4"),
	}

	hero := strings.Split(branding.HeroBlock(m.version), "\n")
	capsW := maxWidth(caps)
	axis := maxWidth(hero)
	if capsW > axis {
		axis = capsW
	}

	var body []string
	for _, l := range hero {
		body = append(body, hcenter(l, axis))
	}
	body = append(body, "", "")
	capsPad := strings.Repeat(" ", (axis-capsW)/2)
	for _, c := range caps {
		body = append(body, capsPad+c)
	}
	return kit.Center(strings.Join(body, "\n"), m.width, m.height)
}

// hcenter centers one rendered line within axis visible cells.
func hcenter(line string, axis int) string {
	gap := axis - visibleWidth(line)
	if gap <= 0 {
		return line
	}
	return strings.Repeat(" ", gap/2) + line
}

func maxWidth(lines []string) int {
	max := 0
	for _, l := range lines {
		if w := visibleWidth(l); w > max {
			max = w
		}
	}
	return max
}

func visibleWidth(s string) int { return len([]rune(ansi.Strip(s))) }

func capLine(name, desc, milestone string) string {
	return "  " + theme.TabActive().Render(padTo(name, 12)) +
		theme.TextDim().Render(desc) +
		"  " + theme.Hint().Render(milestone)
}

func padTo(s string, w int) string {
	if n := len([]rune(s)); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}
