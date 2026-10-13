package settings

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/credstore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- SECRETS section (F-089): names, scopes and users — never values ----

var secretScopeOpts = []string{"this workspace", "every workspace"}

func (m *Model) secretRoot() string {
	if m.d.WS == nil {
		return ""
	}
	return m.d.WS.Root
}

func (m *Model) secretList() []credstore.Secret {
	if m.d.Secrets == nil {
		return nil
	}
	list, _ := m.d.Secrets.Secrets(m.secretRoot())
	return list
}

// secretUsers names the employees allowed to use a secret.
func (m *Model) secretUsers(name string) []string {
	if m.d.Grants == nil {
		return nil
	}
	var out []string
	for _, g := range m.d.Grants.Grants() {
		if g.Scope == tools.AccessSecret+name {
			out = append(out, g.Agent)
		}
	}
	return out
}

func (m *Model) secretsKey(key string) bool {
	if m.d.Secrets == nil {
		return false
	}
	list := m.secretList()
	switch key {
	case "j", "down":
		if m.secretCur < len(list)-1 {
			m.secretCur++
		}
		return true
	case "k", "up":
		if m.secretCur > 0 {
			m.secretCur--
		}
		return true
	case "a", "n":
		m.openFormDialog("add a secret", dlgSecretAdd, "",
			kit.NewTextField("name", ""), kit.NewSecretField("value"),
			kit.NewToggleField("for", secretScopeOpts, 0))
		return true
	case "e", "enter":
		if m.secretCur < len(list) {
			sc := list[m.secretCur]
			at := 0
			if sc.Workspace == "" {
				at = 1
			}
			m.openFormDialog("replace "+sc.Name, dlgSecretSet, sc.Name,
				kit.NewTextField("name", sc.Name), kit.NewSecretField("new value"),
				kit.NewToggleField("for", secretScopeOpts, at))
		}
		return true
	case "x", "d":
		if m.secretCur < len(list) {
			sc := list[m.secretCur]
			m.openConfirmDialog("delete "+sc.Name, "Delete "+sc.Name+" ("+scopeLabel(sc)+")? Employees using it lose it on their next run.",
				sc.Workspace+"|"+sc.Name, dlgSecretDelete)
		}
		return true
	}
	return false
}

func scopeLabel(sc credstore.Secret) string {
	if sc.Workspace == "" {
		return secretScopeOpts[1]
	}
	return secretScopeOpts[0]
}

func (m *Model) submitSecret() {
	v := m.dform.Values()
	name, value := strings.TrimSpace(v[0]), strings.TrimSpace(v[1])
	if !credstore.ValidName(name) {
		m.dform.SetError("name must be an env var name, e.g. GITHUB_TOKEN")
		return
	}
	if value == "" {
		m.dform.SetError("type the value (it stays masked)")
		return
	}
	root := m.secretRoot()
	if v[2] == secretScopeOpts[1] {
		root = ""
	}
	if err := m.d.Secrets.SetIn(root, name, value); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.closeDialog()
	m.flash = "saved " + name + " — employees ask for it by name (secret_request)"
}

func (m *Model) deleteSecret(target string) {
	root, name, _ := strings.Cut(target, "|")
	if err := m.d.Secrets.DeleteIn(root, name); err != nil {
		m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
		return
	}
	m.closeDialog()
	m.flash = "deleted " + name
	if m.secretCur > 0 {
		m.secretCur--
	}
}

func (m *Model) secretsView(w int) []string {
	if m.d.Secrets == nil {
		return []string{theme.TextDim().Render("(secrets unavailable — no user config dir)")}
	}
	var out []string
	for _, l := range kit.WrapWords("Credentials your employees asked for. Values are never shown. "+
		"An employee you allow gets $NAME in DHI's run tool, and output that echoes a value shows ‹secret:NAME›.", max(w, 20)) {
		out = append(out, theme.TextDim().Render(l))
	}
	out = append(out, "")
	list := m.secretList()
	if len(list) == 0 {
		return append(out, theme.Hint().Render("none yet — a add, or an employee asks with secret_request"))
	}
	for i, sc := range list {
		users := m.secretUsers(sc.Name)
		who := "nobody allowed yet"
		if len(users) > 0 {
			who = "used by " + strings.Join(users, ", ")
		}
		cur := "  "
		style := theme.TextStyle()
		if i == m.secretCur {
			cur = theme.AccentText().Render(theme.GlyphCursor) + " "
			style = theme.TabActive()
		}
		out = append(out, cur+style.Render(padTo(sc.Name, 24))+theme.TextDim().Render(padTo(scopeLabel(sc), 17))+theme.Hint().Render(who))
	}
	return out
}
