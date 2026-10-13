package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// ---- CONVENTIONS section (F-075): naming templates ----
//
// A template names task branches and worktree folders, shapes commit
// subjects and fills PR titles and descriptions. DHI ships a few; the
// team adds its own. Applying one copies its rules into the conventions
// every agent and the board use; changes save at once to the tracked
// workspace conventions file (the user file outside a workspace).

var commitFormatChoices = []string{conventions.FormatFree, conventions.FormatConventional, conventions.FormatTicket}

func (m *Model) convNames() []string { return m.cfg.Conventions.TemplateNames() }

func (m *Model) convSelected() (string, conventions.Template, bool) {
	names := m.convNames()
	if len(names) == 0 {
		return "", conventions.Template{}, false
	}
	m.convCur = min(max(m.convCur, 0), len(names)-1)
	name := names[m.convCur]
	t, builtin, _ := m.cfg.Conventions.Lookup(name)
	return name, t, builtin
}

func (m *Model) conventionsKey(key string) bool {
	names := m.convNames()
	switch key {
	case "j", "down":
		if m.convCur < len(names)-1 {
			m.convCur++
		}
	case "k", "up":
		if m.convCur > 0 {
			m.convCur--
		}
	case "enter", "a":
		name, _, _ := m.convSelected()
		next := m.cfg.Conventions
		if err := next.Activate(name); err != nil {
			m.flash = "failed: " + err.Error()
			return true
		}
		if m.saveConventions(next, "using template "+name) {
			m.cfg.Conventions = next
		}
	case "n":
		name, t, _ := m.convSelected()
		m.openTemplateDialog("new template (copy of "+name+")", dlgConvNew, "", t)
	case "e":
		name, t, builtin := m.convSelected()
		if builtin {
			m.flash = "built-in templates are fixed — n copies " + name + " into one you can edit"
			return true
		}
		m.openTemplateDialog("edit template "+name, dlgConvEdit, name, t)
	case "d", "x":
		name, _, builtin := m.convSelected()
		if builtin {
			m.flash = "built-in templates cannot be deleted"
			return true
		}
		m.openConfirmDialog("delete template", "Delete the template "+name+"? Rules already applied stay.", name, dlgConvDelete)
	default:
		return false
	}
	return true
}

// openTemplateDialog edits every field of a template in one form.
func (m *Model) openTemplateDialog(title string, kind dialogKind, target string, t conventions.Template) {
	fi := 0
	for i, f := range commitFormatChoices {
		if f == t.Commit.Format {
			fi = i
		}
	}
	m.openFormDialog(title, kind, target,
		kit.NewTextField("name          ", target),
		kit.NewTextField("description   ", t.Description),
		kit.NewTextField("task branch   ", t.Branch.Task),
		kit.NewTextField("review branch ", t.Branch.Review),
		kit.NewTextField("worktree      ", t.Worktree.Task),
		kit.NewToggleField("commit format ", commitFormatChoices, fi),
		kit.NewTextField("ticket regexp ", t.Commit.TicketPattern),
		kit.NewTextField("commit subject", t.Commit.Subject),
		kit.NewTextField("PR title      ", t.PR.Title),
		kit.NewTextField("PR body       ", strings.ReplaceAll(t.PR.Body, "\n", `\n`)),
	)
}

// submitTemplate validates the form as a template and saves it (and
// re-applies it when it is the active one).
func (m *Model) submitTemplate() {
	v := m.dform.Values()
	name := strings.TrimSpace(v[0])
	if !conventions.ValidTemplateName(name) {
		m.dform.SetError("name: lowercase letters, digits and dashes (max 32)")
		return
	}
	if _, builtin, ok := m.cfg.Conventions.Lookup(name); ok && (builtin || name != m.dtarget) {
		m.dform.SetError("a template named " + name + " already exists")
		return
	}
	var t conventions.Template
	t.Description = strings.TrimSpace(v[1])
	t.Branch.Task, t.Branch.Review = strings.TrimSpace(v[2]), strings.TrimSpace(v[3])
	t.Worktree.Task = strings.TrimSpace(v[4])
	t.Commit.Format, t.Commit.TicketPattern, t.Commit.Subject = v[5], strings.TrimSpace(v[6]), strings.TrimSpace(v[7])
	t.PR.Title, t.PR.Body = strings.TrimSpace(v[8]), strings.ReplaceAll(strings.TrimSpace(v[9]), `\n`, "\n")

	next := m.cfg.Conventions
	tpls := map[string]conventions.Template{}
	for k, x := range next.Templates {
		if k != m.dtarget {
			tpls[k] = x
		}
	}
	tpls[name] = t
	next.Templates = tpls
	if next.Active == m.dtarget && m.dtarget != "" {
		next.Active = name
		next.Apply(t)
	}
	if err := next.Validate(); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	if m.saveConventions(next, "template "+name+" saved") {
		m.cfg.Conventions = next
		m.closeDialog()
		for i, n := range m.convNames() {
			if n == name {
				m.convCur = i
			}
		}
	}
}

func (m *Model) deleteTemplate(name string) {
	next := m.cfg.Conventions
	tpls := map[string]conventions.Template{}
	for k, x := range next.Templates {
		if k != name {
			tpls[k] = x
		}
	}
	next.Templates = tpls
	if next.Active == name {
		next.Active = ""
	}
	if m.saveConventions(next, "template "+name+" deleted") {
		m.cfg.Conventions = next
	}
	m.closeDialog()
}

// conventionsFile is where this section saves: the tracked workspace
// conventions (the team's contract) inside a workspace, else the user's.
func (m *Model) conventionsFile() string {
	if m.d.WS != nil {
		return filepath.Join(m.d.WS.Root, workspace.DHIDir, settings.ConventionsFile)
	}
	if m.savePath != "" {
		return settings.ConventionsPath(m.savePath)
	}
	return ""
}

func (m *Model) saveConventions(c conventions.Config, ok string) bool {
	path := m.conventionsFile()
	if path == "" {
		m.flash = "failed: no conventions file to save to"
		return false
	}
	if err := settings.SaveConventions(path, c); err != nil {
		m.flash = "failed: " + err.Error()
		return false
	}
	m.flash = ok + " → " + m.displayConvPath(path) + " (live)"
	return true
}

func (m *Model) displayConvPath(p string) string {
	if m.d.WS != nil {
		if rel, err := filepath.Rel(m.d.WS.Root, p); err == nil {
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + rel
		}
	}
	return p
}

// conventionsView lists templates (active marked) over a live preview
// of the selected one applied to the current rules.
func (m *Model) conventionsView(w int) []string {
	c := m.cfg.Conventions
	var out []string
	for _, l := range kit.WrapWords("Naming templates: task branches, worktree folders, commit subjects, PR title and description. Enter uses one; n copies it into a template you can edit.", max(w, 20)) {
		out = append(out, theme.TextDim().Render(l))
	}
	out = append(out, "")
	active := c.Active
	if active == "" {
		active = "default" // nothing applied yet: the shipped defaults are in force
	}
	names := m.convNames()
	_, _, _ = m.convSelected()
	for i, name := range names {
		t, builtin, _ := c.Lookup(name)
		mark := "  "
		if name == active {
			mark = theme.SuccessText().Render("● ")
		}
		kind := theme.TextMuted().Render("yours   ")
		if builtin {
			kind = theme.TextMuted().Render("built-in")
		}
		label := padRight(name, 14)
		row := mark + label + " " + kind + "  " + theme.TextDim().Render(t.Description)
		if i == m.convCur {
			row = theme.AccentText().Render(theme.GlyphCursor) + theme.TabActive().Render(" "+ansi.Strip(row))
		} else {
			row = "  " + row
		}
		out = append(out, kit.ClipEllipsis(row, w))
	}
	if name, t, _ := m.convSelected(); name != "" {
		probe := c
		probe.Apply(t)
		head := "PREVIEW " + name
		if name == active {
			head += " (in use)"
		}
		out = append(out, "", theme.SectionHeader().Render(head))
		for _, l := range probe.Preview() {
			label, val, _ := strings.Cut(l, "  ")
			style := theme.TextStyle()
			if strings.Contains(val, "✗") {
				style = theme.DangerText()
			}
			out = append(out, "  "+theme.TextDim().Render(padRight(label, 9))+" "+style.Render(kit.ClipEllipsis(strings.TrimSpace(val), max(w-14, 10))))
		}
		out = append(out, "")
		for _, l := range kit.WrapWords(fmt.Sprintf(
			"placeholders: {slug} {member} {ticket} {user} {date} {title} {assignee} {type} {subject}. The ticket is the first match of %s in the card's slug, title, epic or labels, else the slug.",
			c.Commit.TicketPattern), max(w-2, 20)) {
			out = append(out, "  "+theme.Hint().Render(l))
		}
	}
	return out
}

func padRight(s string, n int) string {
	if r := len([]rune(s)); r < n {
		return s + strings.Repeat(" ", n-r)
	}
	return s
}
