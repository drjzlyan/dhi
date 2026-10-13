package settings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/skillprop"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

// ---- suggested skills (F-091): review, then install where you choose ----

var skillAttach = []string{"yes", "no"}

// skillLines is everything a human judges before installing: where it
// came from and how it was verified, every skill's text (it goes into
// the prompt of each employee it is added to) and every script in full.
func skillLines(p skillprop.Proposal) []string {
	lines := []string{
		fmt.Sprintf("%s %s — suggested by %s", p.Entry, p.Version, p.Agent),
		"why: " + p.Reason,
		"source: " + p.Source,
		"trust: " + p.Trust(),
		"sha256: " + p.SHA256,
	}
	lines = append(lines, p.Ignored...)
	for _, k := range p.Skills {
		lines = append(lines, "", fmt.Sprintf("── skill %s: %s — %s", k.Slug, k.Name, k.Description))
		lines = append(lines, strings.Split(strings.TrimRight(k.Body, "\n"), "\n")...)
		if k.Script != "" {
			lines = append(lines, "", "── script "+k.Script+" (runs sandboxed when an employee calls skill_run):")
			for _, l := range strings.Split(strings.TrimRight(k.ScriptText, "\n"), "\n") {
				lines = append(lines, "  "+l)
			}
		}
	}
	return append(lines, "", "y installs it (you choose where) · n declines")
}

func (m *Model) openSkillInstall(p skillprop.Proposal) {
	if m.d.Registry == nil || m.d.WS == nil {
		m.flash = "failed: the registry is unavailable here"
		return
	}
	title := "install " + p.Entry
	if p.HasScripts() {
		title += " — it ships a script (v to read it)"
	}
	m.openFormDialog(title, dlgSkillInstall, fmt.Sprint(p.ID),
		kit.NewToggleField("install for", []string{skillprop.ScopeWorkspace, skillprop.ScopeEverywhere}, 0),
		kit.NewToggleField("add to "+p.Agent, skillAttach, 0))
}

func (m *Model) submitSkillInstall() {
	var p *skillprop.Proposal
	for _, q := range m.d.SkillProps.Pending() {
		if fmt.Sprint(q.ID) == m.dtarget {
			q := q
			p = &q
		}
	}
	if p == nil {
		m.dform.SetError("the suggestion is gone")
		return
	}
	v := m.dform.Values()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	slugs, err := skillprop.Install(ctx, m.d.Registry, m.d.WS, *p, v[0])
	if err != nil {
		m.dform.SetError(err.Error())
		return
	}
	note := ""
	if v[1] == "yes" && m.d.Org != nil {
		if err := skillprop.Attach(m.d.WS, m.d.Org, p.Agent, slugs); err != nil {
			note = " (could not add it to " + p.Agent + ": " + err.Error() + ")"
		} else {
			note = "; " + p.Agent + " uses it from its next turn"
		}
	}
	if err := m.d.SkillProps.Resolve(p.ID, skillprop.Approved, v[0]); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.flash = fmt.Sprintf("installed %s for %s%s", strings.Join(slugs, ", "), v[0], note)
}
