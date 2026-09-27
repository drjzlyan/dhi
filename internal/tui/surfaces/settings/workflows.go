package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/workflow"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- WORKFLOWS section (F-031): browse + author the layered workflows ----

type wfRow struct {
	slug      string
	title     string
	steps     int
	local     bool // a .dhi/workflows file (not the builtin)
	isDefault bool // the workspace default
	err       string
}

func (m *Model) workflowRows() []wfRow {
	if m.d.WS == nil {
		return nil
	}
	avail, err := workflow.Available(m.d.WS.Root)
	if err != nil {
		return []wfRow{{err: err.Error()}}
	}
	def, _ := workflow.WorkspaceDefault(m.d.WS.Root)
	rows := make([]wfRow, 0, len(avail))
	for _, slug := range avail {
		r := wfRow{slug: slug, isDefault: slug == def, local: !workflow.IsBuiltin(slug)}
		if d, lerr := workflow.Load(m.d.WS.Root, slug); lerr != nil {
			r.err = lerr.Error()
		} else {
			r.title, r.steps = d.Title, len(d.Steps)
		}
		rows = append(rows, r)
	}
	return rows
}

func (m *Model) workflowsKey(key string) bool {
	rows := m.workflowRows()
	switch key {
	case "j", "down":
		if m.wfCur < len(rows)-1 {
			m.wfCur++
		}
		return true
	case "k", "up":
		if m.wfCur > 0 {
			m.wfCur--
		}
		return true
	case "n":
		m.openFormDialog("new workflow", dlgWorkflowNew, "",
			kit.NewTextField("slug ", ""),
			kit.NewTextField("title", ""),
		)
		return true
	case "d":
		if m.d.WS == nil || m.wfCur >= len(rows) || rows[m.wfCur].slug == "" {
			return true
		}
		r := rows[m.wfCur]
		next := r.slug
		if r.isDefault {
			next = ""
		}
		if err := workflow.SaveDefault(m.d.WS.Root, next); err != nil {
			m.flash = "failed: " + err.Error()
		} else if next == "" {
			m.flash = "workspace default workflow cleared"
		} else {
			m.flash = "workspace default workflow: " + next
		}
		return true
	case "v":
		if m.wfCur < len(rows) && rows[m.wfCur].slug != "" {
			m.previewWorkflow(rows[m.wfCur].slug)
		}
		return true
	}
	return false
}

func (m *Model) previewWorkflow(slug string) {
	if m.d.WS == nil {
		return
	}
	def, err := workflow.Load(m.d.WS.Root, slug)
	if err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	lines := []string{theme.Brand().Render(def.Slug) + "  " + theme.TextDim().Render(def.Title)}
	for _, s := range def.Steps {
		g := theme.TextDim().Render("[" + s.Gate + "]")
		if s.Gate != workflow.GateWarn {
			g = theme.SuccessText().Render("[" + s.Gate + "]")
		}
		lines = append(lines, "  "+g+" "+s.ID+
			theme.TextDim().Render("  bind="+s.Bind))
	}
	m.openDisplayDialog("workflow "+slug, lines)
}

func (m *Model) submitWorkflowNew() {
	if m.d.WS == nil {
		m.closeDialog()
		m.flash = "failed: not inside a workspace"
		return
	}
	vals := m.dform.Values()
	slug := strings.TrimSpace(vals[0])
	title := strings.TrimSpace(vals[1])
	if title == "" {
		title = slug
	}
	def := workflow.NewDefinition(slug, title)
	if err := workflow.Save(m.d.WS.Root, def); err != nil {
		m.flash = "failed: " + err.Error()
		return
	}
	m.closeDialog()
	m.flash = "workflow " + slug + " created — edit .dhi/workflows/" + slug + ".toml"
}

func (m *Model) workflowsView() []string {
	if m.d.WS == nil {
		return []string{theme.Hint().Render("workflows unavailable — not inside a workspace")}
	}
	rows := m.workflowRows()
	out := []string{theme.TextDim().Render(
		"Feature workflows (F-031). Layers agent → team → workspace default → builtin; enforced at DHI's seams.")}
	for i, r := range rows {
		if r.err != "" {
			out = append(out, theme.DangerText().Render("! "+r.err))
			continue
		}
		marker := "  "
		if r.isDefault {
			marker = "* "
		}
		where := "builtin"
		if r.local {
			where = "local"
		}
		line := marker + r.slug + "  " +
			fmt.Sprintf("%d steps · %s", r.steps, where)
		if r.title != "" {
			line += "  " + r.title
		}
		if i == m.wfCur {
			out = append(out, theme.GlyphCursor+" "+theme.TabActive().Render(line))
		} else {
			out = append(out, "  "+theme.TextDim().Render(line))
		}
	}
	if len(rows) == 0 {
		out = append(out, theme.Hint().Render("no workflows"))
	}
	return out
}
