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
		if m.patchConventions(next, flatKeys(next, true), nil, "using template "+name) {
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
	case "p":
		m.openFormDialog("learn from an example", dlgConvExample, "",
			kit.NewTextField("commit", ""), kit.NewTextField("PR", ""))
	case "g":
		if m.d.LearnConventions == nil {
			m.flash = "learning from git history is unavailable here"
			return true
		}
		inf := m.d.LearnConventions()
		if inf.Empty() {
			m.flash = "not enough history to learn from yet — p learns from an example instead"
			return true
		}
		m.confirmLearn("learned from your repo", inf)
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
		kit.NewTextField("commit body   ", strings.ReplaceAll(t.Commit.Body, "\n", `\n`)),
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
	t.Commit.Body = strings.ReplaceAll(strings.TrimSpace(v[8]), `\n`, "\n")
	t.PR.Title, t.PR.Body = strings.TrimSpace(v[9]), strings.ReplaceAll(strings.TrimSpace(v[10]), `\n`, "\n")

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
	tpl := map[string]*conventions.Template{name: &t}
	if m.dtarget != "" && m.dtarget != name {
		tpl[m.dtarget] = nil // renamed
	}
	var set map[string]any
	if next.Active == name {
		set = flatKeys(next, true)
	}
	if m.patchConventions(next, set, tpl, "template "+name+" saved") {
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
	var set map[string]any
	if next.Active == name {
		next.Active = ""
		set = map[string]any{"active": ""}
	}
	if m.patchConventions(next, set, map[string]*conventions.Template{name: nil}, "template "+name+" deleted") {
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

// patchConventions validates next (the whole effective rules) and writes
// only the keys this action owns into the conventions file.
func (m *Model) patchConventions(next conventions.Config, set map[string]any, tpls map[string]*conventions.Template, ok string) bool {
	return m.patchConventionsAt(m.conventionsFile(), next, set, tpls, ok)
}

// patchConventionsAt is patchConventions into a named conventions file.
func (m *Model) patchConventionsAt(path string, next conventions.Config, set map[string]any, tpls map[string]*conventions.Template, ok string) bool {
	if path == "" {
		m.flash = "failed: no conventions file to save to"
		return false
	}
	if err := next.Validate(); err != nil {
		m.flash = "failed: " + err.Error()
		return false
	}
	if err := settings.PatchConventions(path, set, tpls); err != nil {
		m.flash = "failed: " + err.Error()
		return false
	}
	m.flash = ok + " → " + m.displayConvPath(path) + " (live)"
	return true
}

// flatKeys are the rules a template sets, as conventions keys (plus
// active when withActive).
func flatKeys(c conventions.Config, withActive bool) map[string]any {
	set := map[string]any{
		"branch.task": c.Branch.Task, "branch.review": c.Branch.Review, "worktree.task": c.Worktree.Task,
		"commit.format": c.Commit.Format, "commit.ticket_pattern": c.Commit.TicketPattern,
		"commit.subject": c.Commit.Subject, "commit.body": c.Commit.Body, "pr.title": c.PR.Title, "pr.body": c.PR.Body,
	}
	if withActive {
		set["active"] = c.Active
	}
	return set
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
	for _, l := range kit.WrapWords("Naming templates: task branches, worktree folders, commit subjects, PR title and description. Enter uses one; n copies it into a template you can edit; p learns your style from a commit or PR you paste, g from your git history.", max(w, 20)) {
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

// submitExample learns from a pasted commit message and/or PR (F-082).
func (m *Model) submitExample() {
	v := m.dform.Values()
	unesc := func(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), `\n`, "\n") }
	inf := conventions.InferFromExample(unesc(v[0]), unesc(v[1]))
	if inf.Empty() {
		m.dform.SetError("paste a commit message, a PR description, or both (\\n for new lines)")
		return
	}
	m.confirmLearn("learned from your example", inf)
}

// confirmLearn shows what would change; enter saves it.
func (m *Model) confirmLearn(title string, inf conventions.Inference) {
	next := m.cfg.Conventions
	next.Learn(inf)
	lines := []string{}
	for _, e := range inf.Evidence {
		lines = append(lines, "• "+e)
	}
	lines = append(lines, "")
	for _, l := range next.Preview() {
		lines = append(lines, "  "+l)
	}
	if b := strings.TrimSpace(next.Commit.Body); b != "" {
		lines = append(lines, "  body      "+strings.ReplaceAll(b, "\n", " ⏎ "))
	}
	lines = append(lines, "", theme.Hint().Render("enter save · esc cancel"))
	m.learned = &inf
	m.dform = nil
	m.dlg = &kit.Modal{Title: title, Lines: lines, Width: min(max(m.width-24, 40), 96)}
	m.dkind = dlgConvLearn
	m.dtarget = ""
}

// saveLearned writes the learned rules as the team's conventions.
func (m *Model) saveLearned() {
	if m.learned == nil {
		m.closeDialog()
		return
	}
	next := m.cfg.Conventions
	next.Learn(*m.learned)
	set := map[string]any{
		"commit.format": next.Commit.Format, "commit.ticket_pattern": next.Commit.TicketPattern,
		"commit.subject": next.Commit.Subject, "commit.max_subject": next.Commit.MaxSubject,
		"commit.body": next.Commit.Body, "pr.body": next.PR.Body,
	}
	if m.patchConventions(next, set, nil, "commit and PR style saved") {
		m.cfg.Conventions = next
	}
	m.learned = nil
	m.closeDialog()
}
