package workspace

import (
	"errors"
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/tui/branding"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func manifestAgent(id, name, model, system string) *manifest.Agent {
	return &manifest.Agent{ID: id, Name: name, Model: model, System: system}
}

func fmtErr(msg string) error { return errors.New(msg) }

// View renders the operations floor or the brand hero when not inside
// a DHI workspace.
func (m *Model) View() string {
	if m.ws == nil {
		lines := strings.Split(branding.HeroBlock(m.version), "\n")
		lines = append(lines, "", theme.Hint().Render("not inside a DHI workspace"))
		return kit.Center(strings.Join(lines, "\n"), maxInt(m.width, 40), maxInt(m.height, 10))
	}
	body := m.sectionStrip() + "\n" + m.activeSection()
	if m.form.kind != fNone {
		body = m.modalView(body)
	}
	return kit.Center(body, maxInt(m.width, 40), maxInt(m.height, 10))
}

// sectionStrip renders the switchable pane tabs with the active one lit.
func (m *Model) sectionStrip() string {
	var parts []string
	for s := sectionID(0); s < secCount; s++ {
		label := s.label()
		if s == m.sec {
			parts = append(parts, theme.TabActive().Render("["+label+"]"))
		} else {
			parts = append(parts, theme.TextDim().Render(label))
		}
	}
	line := strings.Join(parts, theme.TextDim().Render(" · "))
	flash := ""
	if m.form.flash != "" {
		flash = "   " + theme.SuccessText().Render(m.form.flash)
	}
	return line + flash
}

func (m *Model) activeSection() string {
	switch m.sec {
	case secMembers:
		return m.membersBody()
	case secOrg:
		return m.orgBody()
	case secPacks:
		return m.packsBody()
	case secChannels:
		return strings.Join(m.pane.render(maxInt(m.width-8, 40),
			maxInt(m.height-10, 12)), "\n")
	default:
		return m.standardsBody()
	}
}

const nameCol = 14

func cursorGlyph(active bool) string {
	if active {
		return string(theme.GlyphCursor) + " "
	}
	return "  "
}

func (m *Model) membersBody() string {
	members := m.ws.Members()
	c := m.cursors[secMembers]
	clampCursor(&c, len(members))

	var rows []string
	rows = append(rows, theme.Hint().Render("member repos")+theme.TextDim().Render(
		"                    a add · r rename · d remove"))
	if len(members) == 0 {
		rows = append(rows, theme.TextDim().Render("(none — press a to add one)"))
	}
	for i, mem := range members {
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		rows = append(rows, cursorGlyph(i == c)+
			style.Render(padTo(mem.Name, nameCol))+
			theme.Hint().Render(shorten(mem.Path, 46)))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) orgBody() string {
	c := m.cursors[secOrg]
	clampCursor(&c, m.orgItemCount())
	teams, active, archived := m.orgRows()

	var rows []string
	rows = append(rows, theme.Hint().Render("teams & crew")+theme.TextDim().Render(
		"            t team · A agent · x del/archive · R restore"))
	if m.orgErr != "" {
		rows = append(rows, theme.DangerText().Render("org unavailable: "+m.orgErr))
	}

	if len(teams) == 0 && len(active) == 0 && len(archived) == 0 {
		rows = append(rows, theme.TextDim().Render("(empty — press t or A)"))
	}
	idx := 0
	for _, tm := range teams {
		members := "(no members)"
		if n := len(tm.Members); n > 0 {
			lead := tm.Lead
			if lead == "" {
				lead = "—"
			}
			members = fmt.Sprintf("lead %s · %d member(s)", lead, n)
		}
		style := theme.TextDim()
		if idx == c {
			style = theme.TabActive()
		}
		rows = append(rows, cursorGlyph(idx == c)+
			style.Render(padTo(tm.Name, nameCol))+theme.Hint().Render(members))
		idx++
	}
	if idx > 0 && len(active)+len(archived) > 0 {
		rows = append(rows, "")
	}
	for _, id := range active {
		style := theme.TextDim()
		if idx == c {
			style = theme.TabActive()
		}
		rows = append(rows, cursorGlyph(idx == c)+style.Render(id)+
			theme.TextDim().Render("  active"))
		idx++
	}
	for _, id := range archived {
		style := theme.TextDim()
		if idx == c {
			style = theme.TabActive()
		}
		rows = append(rows, cursorGlyph(idx == c)+style.Render("[archived] "+id))
		idx++
	}
	return strings.Join(rows, "\n")
}

func (m *Model) packsBody() string {
	names, _ := m.installedNames()
	c := m.cursors[secPacks]
	clampCursor(&c, len(names))

	var rows []string
	rows = append(rows, theme.Hint().Render("marketplace packs")+
		theme.TextDim().Render("         i install · x uninstall"))
	if len(names) == 0 {
		rows = append(rows, theme.TextDim().Render("(none installed — press i)"))
	}
	recs := m.packRecords(names)
	for i, rec := range recs {
		style := theme.TextDim()
		if i == c {
			style = theme.TabActive()
		}
		rows = append(rows, cursorGlyph(i == c)+
			style.Render(padTo(rec.name, nameCol))+
			theme.Hint().Render(rec.detail))
	}
	return strings.Join(rows, "\n")
}

type packRow struct {
	name, detail string
}

func (m *Model) packRecords(names []string) []packRow {
	out := make([]packRow, 0, len(names))
	if m.packs == nil {
		return out
	}
	recs, err := m.packs.Records()
	if err != nil {
		return out
	}
	for _, n := range names {
		detail := ""
		if rec, ok := recs[n]; ok {
			unit := "agent"
			if len(rec.Agents) != 1 {
				unit = "agents"
			}
			detail = fmt.Sprintf("%s · %d %s", rec.Version, len(rec.Agents), unit)
		}
		out = append(out, packRow{name: n, detail: detail})
	}
	return out
}

func (m *Model) standardsBody() string {
	rows := m.standardRows()
	c := m.cursors[secStandards]
	clampCursor(&c, len(rows))

	var out []string
	out = append(out, theme.Hint().Render("coding standards")+
		theme.TextDim().Render("      w ws-layer · t team · g agent · v preview"))
	for i, r := range rows {
		style := theme.TextDim()
		note := fmt.Sprintf("%d rule(s)", r.count)
		switch r.kind {
		case stdTeam:
			if r.count == 0 {
				note += " (inherit workspace)"
			}
		case stdAgent:
			switch r.mode {
			case "":
				note += " (inherit)"
			default:
				note += " · " + r.mode
			}
		}
		if i == c {
			style = theme.TabActive()
		}
		out = append(out, cursorGlyph(i == c)+
			style.Render(padTo(sectionLabel(r), nameCol))+
			theme.Hint().Render(note))
	}
	out = append(out, "", theme.TextDim().Render(
		"built-in defaults always apply on top of these layers"))
	return strings.Join(out, "\n")
}

func sectionLabel(r stdRow) string {
	switch r.kind {
	case stdWorkspace:
		return "workspace"
	case stdTeam:
		return "team " + r.label
	case stdAgent:
		return "agent " + r.label
	default:
		return r.label
	}
}

// ---- modals ----

func (m *Model) modalView(body string) string {
	f := &m.form
	p := kit.NewPanel(modalTitle(f.kind), true)
	p.SetContent(m.modalLines()...)
	return stackOver(dimLines(body), p.View())
}

func (m *Model) modalLines() []string {
	f := &m.form
	switch f.kind {
	case fRemoveConfirm:
		return confirmLines("remove member "+f.target()+"?",
			"unregisters the repo; the working tree",
			"on disk is never deleted.", f)
	case fTeamDeleteConfirm:
		return confirmLines("delete team "+f.target()+"?",
			"membership lists go with it;", "agents themselves are untouched.", f)
	case fAgentArchiveConfirm:
		return confirmLines("archive agent "+f.target()+"?",
			"its manifest moves to .archived/",
			"and it stops receiving turns.", f)
	case fPackUninstallConfirm:
		return confirmLines("uninstall pack "+f.target()+"?",
			"removes exactly the agents this",
			"pack installed.", f)
	case fStdPreviewShow:
		lines := []string{theme.TextDim().Render("effective instructions"), ""}
		lines = append(lines, f.preview...)
		return append(lines, "", theme.Hint().Render("any key closes"))
	}

	lines := make([]string, 0, len(f.fields)*2)
	if f.orig != "" && f.kind != fAdd {
		lines = append(lines, theme.TextDim().Render("editing: "+f.target()))
	}
	for i, fl := range f.fields {
		lines = append(lines, m.fieldLine(fl, i == f.cur && !f.busy))
		if fl.isToggle() {
			lines = append(lines, theme.TextDim().Render("      ←/→ switches mode"))
		}
	}
	lines = append(lines, "", hintOrErr(f, defaultHint(f.kind)))
	return lines
}

func defaultHint(k modalKind) string {
	switch k {
	case fPackInstall:
		return "local dir or git URL · enter install"
	case fStdLayerEdit:
		return "comma-separated rules · enter save"
	case fStdPreviewPrompt:
		return "enter shows effective block"
	case fAgentNew:
		return "id slug · model required · enter create"
	case fTeamEdit:
		return "lead: you or agent id · enter save"
	}
	return "tab next field · enter save · esc cancel"
}

func confirmLines(question, l1, l2 string, f *formState) []string {
	lines := []string{question,
		theme.TextDim().Render(l1),
		theme.TextDim().Render(l2), ""}
	if f.err != "" {
		lines = append(lines, theme.DangerText().Render(f.err), "")
	}
	return append(lines, theme.Hint().Render("enter confirm · esc keep"))
}

func (m *Model) fieldLine(fl field, focused bool) string {
	cursor := " "
	value := fl.text()
	if fl.isToggle() {
		value = "< " + fl.toggleValue() + " >"
	} else if focused {
		value += "▏"
	}
	style := theme.Hint()
	if focused {
		cursor = string(theme.GlyphCursor)
		style = theme.SuccessText()
	}
	return cursor + " " + style.Render(padTo(fl.label, 15)) + value
}

func hintOrErr(f *formState, hint string) string {
	switch {
	case f.busy:
		return theme.TabActive().Render("working…")
	case f.err != "":
		return theme.DangerText().Render(f.err)
	default:
		return theme.Hint().Render(hint)
	}
}

func modalTitle(k modalKind) string {
	switch k {
	case fAdd:
		return "add member"
	case fRename:
		return "rename member"
	case fRemoveConfirm:
		return "remove member"
	case fTeamEdit:
		return "team"
	case fTeamDeleteConfirm:
		return "delete team"
	case fAgentNew:
		return "new agent"
	case fAgentArchiveConfirm:
		return "archive agent"
	case fPackInstall:
		return "install pack"
	case fPackUninstallConfirm:
		return "uninstall pack"
	case fStdLayerEdit:
		return "edit rules"
	case fStdPreviewPrompt:
		return "preview rules"
	case fStdPreviewShow:
		return "effective rules"
	}
	return ""
}

// stackOver places overlay on top of body, centered, replacing covered
// lines so geometry stays fixed.
func stackOver(body, overlay string) string {
	bl := strings.Split(body, "\n")
	ol := strings.Split(overlay, "\n")
	vOffset := (len(bl) - len(ol)) / 2
	if vOffset < 0 {
		vOffset = 0
	}
	for i, line := range ol {
		y := vOffset + i
		if y >= len(bl) {
			break
		}
		bl[y] = line
	}
	return strings.Join(bl, "\n")
}

func dimLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, theme.TextDim().Render(l))
	}
	return strings.Join(out, "\n")
}

func shorten(p string, n int) string {
	if len(p) <= n {
		return p
	}
	return "…" + p[len(p)-n+1:]
}

func padTo(s string, w int) string {
	if rn := len([]rune(s)); rn < w {
		return s + strings.Repeat(" ", w-rn)
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// stdSnapshotAlias aliases the standards snapshot for in-package tests.
type stdSnapshotAlias = struct {
	Workspace []string
	Teams     map[string][]string
	Agents    map[string]standards.AgentOverride
}

func inspectSnapshot(root string) (*stdSnapshotAlias, error) {
	snap, err := standards.Inspect(root)
	if err != nil {
		return nil, err
	}
	out := &stdSnapshotAlias{
		Workspace: snap.Workspace,
		Teams:     map[string][]string{},
		Agents:    map[string]standards.AgentOverride{},
	}
	for k, v := range snap.Teams {
		out.Teams[k] = v
	}
	for k, v := range snap.Agents {
		out.Agents[k] = v
	}
	return out, nil
}
