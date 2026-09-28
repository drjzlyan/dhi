package settings

import (
	"context"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/pack"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- PACKS section (F-023): install/uninstall + provenance ----

type packRow struct {
	name    string
	version string
	counts  string
	source  string
}

func (m *Model) packRows() []packRow {
	if m.d.WS == nil {
		return nil
	}
	inst := &pack.Installer{WS: m.d.WS}
	recs, err := inst.Records()
	if err != nil {
		return nil
	}
	rows := make([]packRow, 0, len(recs))
	for name, r := range recs {
		rows = append(rows, packRow{
			name: name, version: r.Version, counts: recSummary(r), source: r.Source,
		})
	}
	// sorted by name for stable rendering
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].name < rows[j-1].name; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	return rows
}

// recSummary renders a pack's provenance as "1 agent · 2 roles · …".
func recSummary(r pack.PackRec) string {
	var parts []string
	add := func(n int, singular, plural string) {
		if n == 0 {
			return
		}
		if n == 1 {
			parts = append(parts, "1 "+singular)
		} else {
			parts = append(parts, itoa(n)+" "+plural)
		}
	}
	add(len(r.Agents), "agent", "agents")
	add(len(r.Roles), "role", "roles")
	add(len(r.Skills), "skill", "skills")
	add(len(r.Workflows), "workflow", "workflows")
	add(len(r.Standards), "rule", "rules")
	add(len(r.MCPServers), "mcp", "mcps")
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " · ")
}

func (m *Model) packsKey(key string) bool {
	rows := m.packRows()
	switch key {
	case "j", "down":
		if m.packCur < len(rows)-1 {
			m.packCur++
			m.flash = ""
		}
		return true
	case "k", "up":
		if m.packCur > 0 {
			m.packCur--
			m.flash = ""
		}
		return true
	case "i", "a":
		if m.d.WS == nil {
			return false
		}
		m.openFormDialog("install pack", dlgPackInstall, "",
			kit.NewTextField("source", ""))
		return true
	case "x", "d":
		if m.d.WS == nil || m.packCur >= len(rows) {
			return false
		}
		m.openConfirmDialog("uninstall pack",
			"uninstall pack "+rows[m.packCur].name+" ("+
				rows[m.packCur].counts+")?",
			rows[m.packCur].name, dlgPackUninstall)
		return true
	}
	return false
}

// submitPackInstall kicks the async install; the dialog stays busy on
// the events channel until the outcome lands (clones can take seconds).
func (m *Model) submitPackInstall() {
	f := m.dform
	if m.d.WS == nil {
		f.SetError("pack management unavailable: not inside a workspace")
		return
	}
	src := strings.TrimSpace(f.Values()[0])
	if src == "" {
		f.SetError("path or git URL required")
		return
	}
	f.Busy = true
	f.Err = ""
	installer := &pack.Installer{WS: m.d.WS}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := installer.Install(ctx, src)
		ev := settingsEvent{}
		if err != nil {
			ev.err = err.Error()
		} else {
			ev.msg = "installed pack " + res.Pack + " (" + resultSummary(res) + ")"
		}
		select {
		case m.events <- ev:
		default:
		}
	}()
}

func (m *Model) packsView() []string {
	if m.d.WS == nil {
		return []string{theme.TextDim().Render(
			"(pack management unavailable — not inside a workspace)")}
	}
	rows := m.packRows()
	if len(rows) == 0 {
		return []string{theme.TextDim().Render(
			"(no packs installed — \"i\" to install from a path or git URL)")}
	}
	out := []string{}
	for i, r := range rows {
		line := padTo(r.name, 16) +
			theme.Hint().Render(padTo(orDash(r.version), 10)) +
			theme.TextDim().Render(padTo(r.counts, 26)) +
			theme.TextMuted().Render(r.source)
		if i == m.packCur {
			out = append(out, theme.GlyphCursor+" "+theme.TabActive().Render(line))
		} else {
			out = append(out, "  "+theme.TextDim().Render(line))
		}
	}
	return out
}

// resultSummary renders an install outcome's kinds.
func resultSummary(res *pack.Result) string {
	r := pack.PackRec{Agents: res.Agents, Workflows: res.Workflows, Roles: res.Roles,
		Skills: res.Skills, Standards: res.Standards, MCPServers: res.MCPServers}
	return recSummary(r)
}
