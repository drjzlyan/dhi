package settings

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- STANDARDS section (F-023): layered coding standards ----

type stdRowKind uint8

const (
	stdWorkspace stdRowKind = iota
	stdTeam
	stdAgent
)

type stdRow struct {
	kind  stdRowKind
	label string
	count int
	mode  string // agent rows: extend|replace|"" (no layer yet)
}

func (m *Model) standardRows() []stdRow {
	if m.d.WS == nil {
		return nil
	}
	snap, err := standards.Inspect(m.d.WS.Root)
	if err != nil {
		return []stdRow{{kind: stdWorkspace, label: "workspace", count: 0}}
	}
	rows := []stdRow{{kind: stdWorkspace, label: "workspace", count: len(snap.Workspace)}}
	if m.d.Org != nil {
		for _, t := range m.d.Org.Teams() {
			rows = append(rows, stdRow{kind: stdTeam, label: t.Name,
				count: len(snap.Teams[t.Name])})
		}
	}
	for _, id := range m.rosterIDs() {
		count, mode := 0, ""
		if ov, ok := snap.Agents[id]; ok {
			count, mode = len(ov.Entries), ov.Mode
		}
		rows = append(rows, stdRow{kind: stdAgent, label: id, count: count, mode: mode})
	}
	return rows
}

func (m *Model) standardsKey(key string) bool {
	rows := m.standardRows()
	if m.d.WS == nil {
		return key == "j" || key == "k" || key == "down" || key == "up"
	}
	switch key {
	case "j", "down":
		if m.stdCur < len(rows)-1 {
			m.stdCur++
			m.flash = ""
		}
		return true
	case "k", "up":
		if m.stdCur > 0 {
			m.stdCur--
			m.flash = ""
		}
		return true
	case "w":
		snap, _ := standards.Inspect(m.d.WS.Root)
		m.openFormDialog("workspace layer", dlgStdLayer, "@workspace",
			kit.NewTextField("rules (csv)", strings.Join(snap.Workspace, ", ")))
		return true
	case "t":
		if m.stdCur < len(rows) && rows[m.stdCur].kind == stdTeam {
			slug := rows[m.stdCur].label
			snap, _ := standards.Inspect(m.d.WS.Root)
			m.openFormDialog("team layer "+slug, dlgStdLayer, "@team:"+slug,
				kit.NewTextField("rules (csv)", strings.Join(snap.Teams[slug], ", ")))
			return true
		}
	case "g":
		if m.stdCur >= len(rows) {
			return false
		}
		r := rows[m.stdCur]
		if r.kind != stdAgent {
			return false
		}
		mode := standards.ModeExtend
		entries := []string(nil)
		if snap, err := standards.Inspect(m.d.WS.Root); err == nil {
			if ov, ok := snap.Agents[r.label]; ok {
				mode, entries = ov.Mode, ov.Entries
			}
		}
		m.openFormDialog("agent layer "+r.label, dlgStdLayer, "@agent:"+r.label,
			kit.NewTextField("agent id", r.label),
			kit.NewToggleField("mode", []string{standards.ModeExtend, standards.ModeReplace},
				modeIndex(mode)),
			kit.NewTextField("rules (csv)", strings.Join(entries, ", ")))
		return true
	case "v":
		// v previews the SELECTED row directly (F-026 P6) — no agent-id
		// typing round-trip.
		if m.stdCur < len(rows) {
			row := rows[m.stdCur]
			switch row.kind {
			case stdAgent:
				m.previewStandards(row.label)
				return true
			case stdTeam:
				m.previewStandards("#" + row.label)
				return true
			default:
				m.previewStandards("")
				return true
			}
		}
	}
	return false
}

func modeIndex(mode string) int {
	if mode == standards.ModeReplace {
		return 1
	}
	return 0
}

func (m *Model) submitStdLayer() {
	f := m.dform
	if m.d.WS == nil {
		f.SetError("standards unavailable: not inside a workspace")
		return
	}
	root := m.d.WS.Root
	vals := f.Values()
	switch {
	case strings.HasPrefix(m.dtarget, "@workspace"):
		if err := standards.Save(root, csvSplit(vals[0]), nil, nil); err != nil {
			f.SetError(err.Error())
			return
		}
	case strings.HasPrefix(m.dtarget, "@team:"):
		slug := strings.TrimSpace(strings.TrimPrefix(m.dtarget, "@team:"))
		if err := standards.Save(root, currentWorkspaceRules(root),
			map[string][]string{slug: csvSplit(vals[0])},
			currentAgentRules(root)); err != nil {
			f.SetError(err.Error())
			return
		}
	case strings.HasPrefix(m.dtarget, "@agent:"):
		id := strings.TrimSpace(vals[0])
		agents := currentAgentRules(root)
		if entries := csvSplit(vals[2]); len(entries) == 0 {
			delete(agents, id)
		} else {
			agents[id] = standards.AgentOverride{Mode: vals[1], Entries: entries}
		}
		if err := standards.Save(root, currentWorkspaceRules(root),
			currentTeamRules(root), agents); err != nil {
			f.SetError(err.Error())
			return
		}
	}
	m.closeDialog()
	m.flash = "standards saved"
}

func (m *Model) showStdPreview() {
	f := m.dform
	if m.d.WS == nil {
		f.SetError("standards unavailable: not inside a workspace")
		return
	}
	id := strings.TrimSpace(f.Values()[0])
	block := standards.Resolve(m.d.WS.Root, id, m.teamLookup())
	m.openDisplayDialog("effective standards — "+orDash(id),
		strings.Split(strings.TrimRight(block, "\n"), "\n"))
}

// previewStandards opens the effective block for id directly (the
// standards `v` path — F-026 P6). Empty id = workspace layer.
func (m *Model) previewStandards(id string) {
	if m.d.WS == nil {
		m.flash = "standards unavailable: not inside a workspace"
		return
	}
	block := standards.Resolve(m.d.WS.Root, id, m.teamLookup())
	m.openDisplayDialog("effective standards — "+orDash(id),
		strings.Split(strings.TrimRight(block, "\n"), "\n"))
}

func (m *Model) teamLookup() standards.TeamLookup {
	if m.d.Org == nil {
		return nil
	}
	return func(agentID string) []string { return m.d.Org.TeamsOf(agentID) }
}

// read-modify-write helpers keep untouched layers intact when saving one.

func currentWorkspaceRules(root string) []string {
	snap, err := standards.Inspect(root)
	if err != nil {
		return nil
	}
	return snap.Workspace
}

func currentTeamRules(root string) map[string][]string {
	out := map[string][]string{}
	if snap, err := standards.Inspect(root); err == nil {
		for slug, v := range snap.Teams {
			out[slug] = v
		}
	}
	return out
}

func currentAgentRules(root string) map[string]standards.AgentOverride {
	out := map[string]standards.AgentOverride{}
	if snap, err := standards.Inspect(root); err == nil {
		for id, ov := range snap.Agents {
			out[id] = ov
		}
	}
	return out
}

func (m *Model) standardsView() []string {
	rows := m.standardRows()
	if m.d.WS == nil || len(rows) == 0 {
		return []string{theme.TextDim().Render(
			"(standards unavailable — not inside a workspace)")}
	}
	out := []string{}
	for i, r := range rows {
		var line string
		switch r.kind {
		case stdWorkspace:
			line = padTo("workspace", 16) + theme.TextDim().Render(itoa(r.count)+" rules")
		case stdTeam:
			line = padTo("#"+r.label, 16) + theme.TextDim().Render(itoa(r.count)+" rules")
		default:
			line = padTo(r.label, 16) + theme.TextDim().Render(itoa(r.count)+" rules") +
				theme.Hint().Render(" "+orDash(r.mode))
		}
		if i == m.stdCur {
			out = append(out, theme.GlyphCursor+" "+theme.TabActive().Render(line))
		} else {
			out = append(out, "  "+theme.TextDim().Render(line))
		}
	}
	return out
}
