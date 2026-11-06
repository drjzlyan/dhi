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
	}
	lines = append(lines, p.Ignored...)
	for _, k := range p.Skills {
		lines = append(lines, "", fmt.Sprintf("── skill %s: %s — %s", k.Slug, k.Name, k.Description))
		lines = append(lines, strings.Split(strings.TrimRight(k.Body, "\n"), "\n")...)
		for _, sc := range k.Scripts {
			lines = append(lines, "", "── script "+sc.Path+" (runs sandboxed when an employee calls skill_run):")
			for _, l := range strings.Split(strings.TrimRight(sc.Text, "\n"), "\n") {
				lines = append(lines, "  "+l)
			}
		}
	}
	return append(lines, "", "y installs it (you choose where) · n declines")
}

func (m *Model) openSkillInstall(p skillprop.Proposal) {
	if m.d.Ext == nil || m.d.WS == nil {
		m.flash = "failed: installs are unavailable here"
		return
	}
	title := "install " + p.Entry
	if p.HasScripts() {
		title += " — it ships a script (v to read it)"
	}
	m.openFormDialog(title, dlgSkillInstall, fmt.Sprint(p.ID),
		kit.NewToggleField("install for", m.d.Ext.scopeNames(), 0),
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
	// The install fetches the skill (network, up to minutes): off the UI
	// thread, with the form busy so a second enter cannot install twice.
	m.dform.SetError("")
	m.dform.Busy = true
	prop, scopeName, attach := *p, v[0], v[1] == "yes"
	fetch, tmp, scope, ch := m.d.Ext.Fetch, m.d.Ext.Tmp, m.d.Ext.scope(v[0]), m.events
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		slugs, err := skillprop.Install(ctx, fetch, tmp, prop, scope)
		ch <- settingsEvent{skill: &skillEvent{prop: prop, scope: scopeName, attach: attach, slugs: slugs, err: err}}
	}()
}

// skillEvent is a finished skill install, back on the UI loop.
type skillEvent struct {
	prop   skillprop.Proposal
	scope  string
	attach bool
	slugs  []string
	err    error
}

// onSkillInstalled records an install that finished: the suggestion is
// resolved and, if asked, the skill joins the agent (local writes).
func (m *Model) onSkillInstalled(ev skillEvent) {
	open := m.dkind == dlgSkillInstall && m.dform != nil && m.dtarget == fmt.Sprint(ev.prop.ID)
	fail := func(msg string) {
		if open {
			m.dform.Busy = false
			m.dform.SetError(msg)
			return
		}
		m.flash = "failed: " + msg
	}
	if ev.err != nil {
		fail(ev.err.Error())
		return
	}
	note := ""
	if ev.attach && m.d.Org != nil {
		if err := skillprop.Attach(m.d.WS, m.d.Org, ev.prop.Agent, ev.slugs); err != nil {
			note = " (could not add it to " + ev.prop.Agent + ": " + err.Error() + ")"
		} else {
			note = "; " + ev.prop.Agent + " uses it from its next turn"
		}
	}
	if err := m.d.SkillProps.Resolve(ev.prop.ID, skillprop.Approved, ev.scope); err != nil {
		fail(err.Error())
		return
	}
	if open {
		m.closeDialog()
	}
	m.flash = fmt.Sprintf("installed %s for %s%s", strings.Join(ev.slugs, ", "), ev.scope, note)
}
