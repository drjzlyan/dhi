package settings

import (
	"fmt"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// PROPOSALS (F-069, F-070): what agents asked the human to approve — new
// tools now, settings changes next — and the forged tools in use. Agents
// never activate anything themselves; this is the gate.

// propItem is one selectable row.
type propItem struct {
	kind  string // "tool" (pending) | "active" (approved tool) | "setting"
	tool  forge.Tool
	calls int
	set   *SettingProposal
}

func (m *Model) propItems() []propItem {
	var out []propItem
	for _, sp := range m.pendingSettings() {
		sp := sp
		out = append(out, propItem{kind: "setting", set: &sp})
	}
	if m.d.Forge == nil {
		return out
	}
	if pend, err := m.d.Forge.Pending(); err == nil {
		for _, t := range pend {
			out = append(out, propItem{kind: "tool", tool: t})
		}
	}
	use := m.d.Forge.Usages()
	if act, err := m.d.Forge.Active(); err == nil {
		for _, t := range act {
			out = append(out, propItem{kind: "active", tool: t, calls: use[t.Name].Calls})
		}
	}
	return out
}

func (m *Model) propSelected() (propItem, bool) {
	items := m.propItems()
	if m.propCur < len(items) {
		return items[m.propCur], true
	}
	return propItem{}, false
}

func (m *Model) proposalsKey(key string) bool {
	items := m.propItems()
	switch key {
	case "j", "down":
		if m.propCur < len(items)-1 {
			m.propCur++
		}
		return true
	case "k", "up":
		if m.propCur > 0 {
			m.propCur--
		}
		return true
	}
	it, ok := m.propSelected()
	if !ok {
		return false
	}
	switch key {
	case "enter", "v":
		if it.kind == "setting" {
			m.openDisplayDialog("proposed setting — "+it.set.Key, it.set.lines())
		} else {
			m.openDisplayDialog(fmt.Sprintf("%s v%d", it.tool.Name, it.tool.Version), toolLines(it.tool))
		}
		return true
	case "y":
		switch it.kind {
		case "tool":
			t, err := m.d.Forge.Approve(it.tool.Name, bus.Human)
			if err != nil {
				m.flash = "failed: " + err.Error()
				return true
			}
			m.flash = fmt.Sprintf("approved — every agent can now call custom__%s (v%d)", t.Name, t.Version)
		case "setting":
			m.applySetting(*it.set)
		}
		return true
	case "n":
		switch it.kind {
		case "tool":
			if err := m.d.Forge.Reject(it.tool.Name); err != nil {
				m.flash = "failed: " + err.Error()
				return true
			}
			m.flash = "rejected " + it.tool.Name
		case "setting":
			m.rejectSetting(*it.set)
		}
		m.propCur = max(0, min(m.propCur, len(m.propItems())-1))
		return true
	case "x":
		if it.kind == "active" {
			if err := m.d.Forge.Archive(it.tool.Name); err != nil {
				m.flash = "failed: " + err.Error()
				return true
			}
			m.flash = "archived " + it.tool.Name + " — no agent can call it"
			m.propCur = max(0, min(m.propCur, len(m.propItems())-1))
			return true
		}
	}
	return false
}

// toolLines is the full definition a human reads before approving.
func toolLines(t forge.Tool) []string {
	lines := []string{
		fmt.Sprintf("%s — %s", t.Name, t.Description),
		fmt.Sprintf("scope %s · %s · version %d · proposed by %s", t.Scope, t.Kind(), t.Version, t.ProposedBy),
		"why: " + orDash(t.Rationale), "",
	}
	if len(t.Inputs) > 0 {
		lines = append(lines, "inputs:")
		for _, in := range t.Inputs {
			req := ""
			if in.Required {
				req = " (required)"
			}
			lines = append(lines, fmt.Sprintf("  %s: %s%s  %s", in.Name, in.Type, req, in.Description))
		}
		lines = append(lines, "")
	}
	if t.Script != "" {
		lines = append(lines, "script (runs sandboxed, no network):")
		for _, l := range strings.Split(strings.TrimRight(t.Script, "\n"), "\n") {
			lines = append(lines, "  "+l)
		}
	} else {
		lines = append(lines, "steps (allowlisted commands, no shell):")
		for i, st := range t.Steps {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, strings.Join(st.Run, " ")))
		}
	}
	return lines
}

// proposalsView renders the section body.
func (m *Model) proposalsView(w int) []string {
	items := m.propItems()
	out := []string{theme.TextDim().Render("what agents asked you to approve · nothing they propose is active until you say y"), ""}
	head := func(title string) {
		if len(out) > 2 {
			out = append(out, "")
		}
		out = append(out, " "+theme.SectionHeader().Render(title)+" "+theme.Rule(max(w-len(title)-4, 0)))
	}
	if m.d.Forge == nil && len(items) == 0 {
		return append(out, theme.Hint().Render("  (proposals unavailable — not inside a workspace)"))
	}
	section := ""
	pendingSeen, activeSeen := false, false
	for i, it := range items {
		want := "PENDING"
		if it.kind == "active" {
			want = "FORGED TOOLS"
		}
		if want != section {
			head(want)
			section = want
		}
		if it.kind == "active" {
			activeSeen = true
		} else {
			pendingSeen = true
		}
		out = append(out, m.propLine(it, i == m.propCur, w))
	}
	if !pendingSeen {
		out = append(out, "", "  "+theme.Hint().Render("no pending proposals"))
	}
	if !activeSeen {
		out = append(out, "  "+theme.Hint().Render("no forged tools yet — agents propose one when they keep repeating the same commands"))
	}
	return out
}

func (m *Model) propLine(it propItem, selected bool, w int) string {
	lead := "  "
	nameStyle := theme.TextStyle()
	if selected {
		lead = theme.GlyphCursor + " "
		nameStyle = theme.TabActive()
	}
	switch it.kind {
	case "setting":
		sp := it.set
		return lead + theme.WarningText().Render(theme.GlyphDiamond) + " " + nameStyle.Render(padTo(sp.Key, 26)) +
			theme.TextMuted().Render(sp.Old) + " → " + theme.SuccessText().Render(sp.New) +
			theme.Hint().Render("  by "+sp.Agent)
	case "tool":
		t := it.tool
		return lead + theme.WarningText().Render(theme.GlyphDiamond) + " " + nameStyle.Render(padTo(t.Name, 18)) +
			theme.Chip().Render(t.Scope) + " " + theme.Hint().Render(fmt.Sprintf("%s · v%d · by %s · ", t.Kind(), t.Version, t.ProposedBy)) +
			theme.TextDim().Render(clip(t.Description, max(w-62, 16)))
	}
	t := it.tool
	return lead + theme.SuccessText().Render(theme.GlyphCheck) + " " + nameStyle.Render(padTo("custom__"+t.Name, 26)) +
		theme.Chip().Render(t.Scope) + " " + theme.Hint().Render(fmt.Sprintf("v%d · %d call(s) · ", t.Version, it.calls)) +
		theme.TextDim().Render(clip(t.Description, max(w-60, 16)))
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
