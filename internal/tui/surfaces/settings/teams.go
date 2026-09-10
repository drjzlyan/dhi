package settings

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- TEAMS section (F-023): the only team UI ----

func (m *Model) teamRows() []org.Team {
	if m.d.Org == nil {
		return nil
	}
	return m.d.Org.Teams()
}

func (m *Model) teamsKey(key string) bool {
	rows := m.teamRows()
	switch key {
	case "j", "down":
		if m.teamCur < len(rows)-1 {
			m.teamCur++
			m.flash = ""
		}
		return true
	case "k", "up":
		if m.teamCur > 0 {
			m.teamCur--
			m.flash = ""
		}
		return true
	case "n":
		if m.d.Org == nil {
			return false
		}
		m.openFormDialog("new team", dlgTeam, "",
			kit.NewTextField("name", ""),
			kit.NewToggleField("lead", m.leadChoices(""), 0),
			kit.NewTextField("members", ""))
		return true
	case "e", "enter":
		if m.d.Org == nil || m.teamCur >= len(rows) {
			return false
		}
		t := rows[m.teamCur]
		leadIx := 0
		for i, c := range m.leadChoices(t.Lead) {
			if c == t.Lead {
				leadIx = i
				break
			}
		}
		m.openFormDialog("edit team "+t.Name, dlgTeam, t.Name,
			kit.NewTextField("name", t.Name),
			kit.NewToggleField("lead", m.leadChoices(t.Lead), leadIx),
			kit.NewTextField("members", strings.Join(t.Members, ", ")))
		return true
	case "x", "d":
		if m.d.Org == nil || m.teamCur >= len(rows) {
			return false
		}
		t := rows[m.teamCur]
		m.openConfirmDialog("delete team",
			"delete team "+t.Name+" and its channel?", t.Name, dlgTeamDelete)
		return true
	}
	return false
}

// leadChoices builds the lead toggle: you first, then the roster.
func (m *Model) leadChoices(current string) []string {
	choices := append([]string{"you"}, m.rosterIDs()...)
	if current != "" && current != "you" {
		found := false
		for _, c := range choices {
			if c == current {
				found = true
				break
			}
		}
		if !found {
			choices = append(choices, current) // dangling lead stays visible
		}
	}
	return choices
}

func (m *Model) submitTeam() {
	f := m.dform
	if m.d.Org == nil {
		f.SetError("team management unavailable: not inside a workspace")
		return
	}
	vals := f.Values()
	slug := strings.TrimSpace(vals[0])
	lead := strings.TrimSpace(vals[1])
	members := csvSplit(vals[2])
	if slug == "" {
		f.SetError("team name required")
		return
	}
	var err error
	if m.dtarget == "" {
		err = m.d.Org.CreateTeam(slug, lead, members)
	} else {
		err = m.d.Org.UpdateTeam(m.dtarget, lead, members)
	}
	if err != nil {
		f.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.flash = "team " + slug + " saved"
}

func (m *Model) teamsView() []string {
	hint := theme.Hint().Render("n new · e edit · x delete")
	if m.d.Org == nil {
		return []string{hint, theme.TextDim().Render(
			"(team management unavailable — not inside a workspace)")}
	}
	rows := m.teamRows()
	if len(rows) == 0 {
		return []string{hint, theme.TextDim().Render("(no teams — \"n\" to create one)")}
	}
	out := []string{hint}
	for i, t := range rows {
		line := padTo(t.Name, 16) +
			theme.Hint().Render(padTo("lead: "+orDash(t.Lead), 18)) +
			theme.TextDim().Render(itoa(len(t.Members))+" members")
		if i == m.teamCur {
			out = append(out, theme.GlyphCursor+" "+theme.TabActive().Render(line))
		} else {
			out = append(out, "  "+theme.TextDim().Render(line))
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
