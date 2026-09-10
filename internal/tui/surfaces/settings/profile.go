package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/profile"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// ---- agent profile (F-023): the INSPECT profile re-hosted ----
// Read-only modal over the profile aggregation; sources degrade
// independently (profile's own rule).

// diskRoster adapts the on-disk roster to profile.Roster.
type diskRoster struct{ agents []*manifest.Agent }

func loadDiskRoster(ws *workspace.Workspace) (diskRoster, error) {
	agents, err := orgLoadRoster(ws)
	return diskRoster{agents: agents}, err
}

func (r diskRoster) AgentIDs() []string {
	ids := make([]string, 0, len(r.agents))
	for _, a := range r.agents {
		ids = append(ids, a.ID)
	}
	return ids
}

func (r diskRoster) Manifest(id string) (*manifest.Agent, bool) {
	for _, a := range r.agents {
		if a.ID == id {
			return a, true
		}
	}
	return nil, false
}

func (m *Model) openProfile(id string) {
	if m.d.WS == nil {
		m.flash = "profile unavailable: not inside a workspace"
		return
	}
	roster, err := loadDiskRoster(m.d.WS)
	if err != nil {
		m.flash = "profile unavailable: " + err.Error()
		return
	}
	p := profile.Build(m.d.WS, profile.Deps{
		Roster: roster, Bus: m.d.Bus, Org: m.d.Org,
		Tasks: m.d.Tasks, Standards: true,
	}, id)
	m.openDisplayDialog("profile — "+id, profileLines(p))
}

func profileLines(p *profile.Profile) []string {
	var out []string
	add := func(s string) {
		if len(out) < 24 {
			out = append(out, s)
		}
	}
	status := "on roster"
	if !p.Found {
		status = "not on the active roster"
	}
	add(theme.TextDim().Render(status) + " · " + p.TaskLine())
	if p.Manifest != nil {
		sys := p.Manifest.System
		if len(sys) > 60 {
			sys = sys[:57] + "…"
		}
		add("model " + p.Manifest.Model +
			theme.Hint().Render("  runtime: "+p.Manifest.Runtime+
				"  tools: "+orDash(strings.Join(p.Manifest.Tools, ","))))
		if sys != "" {
			add(theme.TextDim().Render("system: " + sys))
		}
	}
	if len(p.Teams) > 0 {
		add("teams " + theme.TabActive().Render(strings.Join(p.Teams, ", ")))
	} else {
		add(theme.TextDim().Render("no teams"))
	}
	open := len(p.TasksOpen)
	done := len(p.TasksDone)
	add(fmt.Sprintf("tasks %d open · %d done", open, done))
	for _, t := range p.TasksOpen {
		if len(out) >= 24 {
			break
		}
		add(theme.Hint().Render("  ◦ " + t.Slug + " — " + string(t.Status)))
	}
	if p.StandardsBlock != "" {
		add(theme.TextDim().Render("standards layered"))
	}
	if len(p.RecentActivity) > 0 {
		add(theme.TextDim().Render("recent activity"))
		for _, msg := range p.RecentActivity {
			if len(out) >= 24 {
				break
			}
			text := msg.Text
			if len(text) > 50 {
				text = text[:47] + "…"
			}
			add(theme.Hint().Render("  " + msg.Channel + "  " + text))
		}
	}
	out = append(out, "", theme.Hint().Render("esc close"))
	return out
}
