package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/hooks"
	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// PLUGIN HOOKS (F-179, ADR-0047): a plugin's lifecycle hooks are shell
// commands. The install card shows every one verbatim; they stay off until
// the user turns them on here, per plugin.

// hookRow is one plugin with stored hooks, in the scope it was installed to.
type hookRow struct {
	scope install.Scope
	info  install.HookInfo
}

func (m *Model) hookRows() []hookRow {
	e := m.ext()
	if e == nil {
		return nil
	}
	var out []hookRow
	for _, sc := range []install.Scope{e.Workspace, e.User} {
		infos, err := install.InstalledHooks(sc)
		if err != nil {
			continue
		}
		for _, hi := range infos {
			out = append(out, hookRow{scope: sc, info: hi})
		}
	}
	return out
}

// hookCardLines lists a plugin's hooks for the install card: every command
// verbatim (wrapped, never clipped), then what DHI will not run.
func hookCardLines(set hooks.Set, width int) []string {
	if set.Empty() {
		return nil
	}
	lines := []string{""}
	for _, l := range kit.WrapWords("hooks are shell commands; they stay off until you turn them on in INTEGRATIONS (h):", width) {
		lines = append(lines, theme.WarningText().Render(l))
	}
	for _, h := range set.Hooks {
		head := "   " + string(h.Event)
		if h.Matcher != "" {
			head += " [" + h.Matcher + "]"
		}
		head += fmt.Sprintf(" (stops after %s):", h.Timeout)
		lines = append(lines, theme.AccentText().Render(head))
		for _, l := range hardWrap(h.Command, width-6) {
			lines = append(lines, theme.WarningText().Render("      "+l))
		}
	}
	for _, u := range set.Unsupported {
		what := u.Event
		if u.Type != "" {
			what += " " + u.Type
		}
		for _, l := range kit.WrapWords("not run: "+what+" — "+u.Why, width-3) {
			lines = append(lines, theme.TextMuted().Render("   "+l))
		}
	}
	return lines
}

// hardWrap splits s into lines of at most w runes, keeping its own newlines.
func hardWrap(s string, w int) []string {
	w = max(w, 20)
	var out []string
	for _, line := range strings.Split(s, "\n") {
		r := []rune(line)
		for len(r) > w {
			out = append(out, string(r[:w]))
			r = r[w:]
		}
		out = append(out, string(r))
	}
	return out
}

// hooksSection is the INTEGRATIONS footer: which plugins ship hooks and
// whether they are on.
func (m *Model) hooksSection() []string {
	rows := m.hookRows()
	if len(rows) == 0 {
		return nil
	}
	out := []string{"", theme.TextDim().Render("PLUGIN HOOKS · h turns a plugin's hooks on or off (they are shell commands)")}
	for _, r := range rows {
		state := kit.Badge(theme.BadgeNeutral, "off")
		if r.info.Enabled {
			state = kit.Badge(theme.BadgeOK, "on")
		}
		line := "  " + padTo(kit.ClipEllipsis(r.info.Plugin, 24), 25) + padTo(state, 6) +
			theme.TextMuted().Render(fmt.Sprintf("%d to run, %d not run · %s", len(r.info.Set.Hooks), len(r.info.Set.Unsupported), r.scope.Name))
		if r.info.Problem != "" {
			line += "  " + theme.WarningText().Render(kit.ClipEllipsis(r.info.Problem, 60))
		}
		out = append(out, line)
	}
	return out
}

func (m *Model) openHooksToggle() {
	rows := m.hookRows()
	if len(rows) == 0 {
		m.flash = "no installed plugin ships hooks"
		return
	}
	var fields []kit.Field
	for _, r := range rows {
		i := 0
		if r.info.Enabled {
			i = 1
		}
		fields = append(fields, kit.NewToggleField(r.info.Plugin+" ("+r.scope.Name+")", []string{"off", "on"}, i))
	}
	m.openFormDialog("plugin hooks", dlgPluginHooks, "", fields...)
	m.dlgExtra = []string{"", theme.Hint().Render("hooks run in the sandbox with the calling agent's scopes; an agent without exec scope runs none")}
}

func (m *Model) submitPluginHooks() {
	rows := m.hookRows()
	vals := m.dform.Values()
	var changed []string
	for i, r := range rows {
		if i >= len(vals) {
			break
		}
		on := vals[i] == "on"
		if on == r.info.Enabled {
			continue
		}
		if err := install.SetHooksEnabled(r.scope, r.info.Plugin, on); err != nil {
			m.dform.SetError(err.Error())
			return
		}
		changed = append(changed, r.info.Plugin+(map[bool]string{true: " on", false: " off"})[on])
	}
	m.closeDialog()
	if len(changed) > 0 {
		m.flash = "plugin hooks: " + strings.Join(changed, ", ")
	}
}
