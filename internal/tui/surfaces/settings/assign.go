package settings

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/assign"
	"github.com/drjzlyan/dhi/internal/agentkit/install"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// The Assign dialog (F-107): who gets a skill or an MCP server. DHI
// preselects the bots whose role and description fit, each with its
// reason; "all bots" gives it to every bot, now and later; or the user
// picks.

type assignRow struct {
	id, role, reason string
	on               bool
}

type assignState struct {
	item    assign.Item
	rows    []assignRow
	cur     int // 0 = the "all bots" row, then one per agent
	allBots bool
	rec     map[string]bool
}

// openAssign opens the dialog for item; more are asked next.
func (m *Model) openAssign(item assign.Item, more []assign.Item) {
	m.extState.queue = append(m.extState.queue, more...)
	roster, err := m.roster()
	if err != nil || len(roster) == 0 {
		m.flash += " · no bots yet: give it to them in Settings → AGENTS"
		m.nextAssign()
		return
	}
	picks := assign.Recommend(item, roster, m.libStore(), m.d.Org)
	reason := map[string]string{}
	for _, p := range picks {
		reason[p.AgentID] = p.Reason
	}
	holders := map[string]bool{}
	for _, id := range assign.Holders(roster, item) {
		holders[id] = true
	}
	pol := m.allBotsPolicy()
	list := pol.Skills
	if item.Kind == assign.KindServer {
		list = pol.Servers
	}
	st := &assignState{item: item, rec: map[string]bool{}}
	for _, n := range list {
		st.allBots = st.allBots || n == item.Name
	}
	fresh := len(holders) == 0 && !st.allBots
	for _, a := range roster {
		_, recommended := reason[a.ID]
		st.rec[a.ID] = recommended
		st.rows = append(st.rows, assignRow{id: a.ID, role: a.Role, reason: reason[a.ID],
			on: holders[a.ID] || (fresh && recommended)})
	}
	m.extState.assign = st
}

func (m *Model) nextAssign() {
	q := m.extState.queue
	if len(q) == 0 {
		return
	}
	m.extState.queue = q[1:]
	m.openAssign(q[0], nil)
}

func (m *Model) assignKey(key string) {
	st := m.extState.assign
	switch key {
	case "j", "down", "tab":
		if st.cur < len(st.rows) {
			st.cur++
		}
	case "k", "up", "shift+tab":
		if st.cur > 0 {
			st.cur--
		}
	case " ", "space", "x":
		if st.cur == 0 {
			st.allBots = !st.allBots
		} else {
			st.rows[st.cur-1].on = !st.rows[st.cur-1].on
		}
	case "a":
		st.allBots = !st.allBots
	case "r":
		st.allBots = false
		for i := range st.rows {
			st.rows[i].on = st.rec[st.rows[i].id]
		}
	case "n":
		st.allBots = false
		for i := range st.rows {
			st.rows[i].on = false
		}
	case "esc":
		m.extState.assign = nil
		m.flash = "kept who has " + st.item.Name + " as it was"
		m.nextAssign()
	case "enter":
		m.saveAssign()
	}
}

func (m *Model) saveAssign() {
	st := m.extState.assign
	e := m.ext()
	// The all-bots policy lives in the scope the item is installed in;
	// the workspace's when that is unknown.
	sc := install.Scope{}
	if e != nil {
		sc = e.Workspace
		if sc.LockPath == "" {
			sc = e.User
		}
		if e.User.LockPath != "" {
			if l, err := install.ReadLock(e.User.LockPath); err == nil {
				if _, ok := l.Skills[st.item.Name]; ok && st.item.Kind == assign.KindSkill {
					sc = e.User
				}
				if _, ok := l.Servers[st.item.Name]; ok && st.item.Kind == assign.KindServer {
					sc = e.User
				}
			}
		}
	}
	if sc.LockPath != "" {
		l, err := install.ReadLock(sc.LockPath)
		if err == nil {
			l.SetAllBots(st.item.Kind, st.item.Name, st.allBots)
			err = l.Write(sc.LockPath)
		}
		if err != nil {
			m.flash = "failed: " + err.Error()
			return
		}
	}
	var ids []string
	if !st.allBots {
		for _, r := range st.rows {
			if r.on {
				ids = append(ids, r.id)
			}
		}
	}
	m.extState.assign = nil
	if m.d.WS == nil || m.d.Org == nil {
		m.flash = "failed: agent management unavailable"
		return
	}
	_, _, err := assign.Apply(m.d.WS, m.d.Org, st.item, ids)
	msg := st.item.Name + ": "
	switch {
	case st.allBots:
		msg += "every bot has it, new ones too"
	case len(ids) == 0:
		msg += "no bot has it"
	default:
		msg += strings.Join(ids, ", ") + " have it"
	}
	m.afterCrewWrite(err, msg, "")
	m.nextAssign()
}

// assignView renders the dialog box.
func (m *Model) assignView(w int) string {
	st := m.extState.assign
	inner := min(max(w-12, 40), 96)
	kind := "skill"
	if st.item.Kind == assign.KindServer {
		kind = "server"
	}
	lines := []string{theme.TextDim().Render(kit.ClipEllipsis("which bots should use the "+kind+" "+st.item.Name+"?", inner)), ""}
	row := func(i int, text string) string {
		if i == st.cur {
			return theme.AccentText().Render(theme.GlyphCursor) + " " + text
		}
		return "  " + text
	}
	lines = append(lines, row(0, kit.Checkbox("All bots — including ones you add later", st.allBots, st.cur == 0)), "")
	for i, r := range st.rows {
		label := padTo(r.id, 14) + theme.TextMuted().Render(padTo(orDash(r.role), 16))
		if r.reason != "" {
			label += theme.SuccessText().Render("recommended ") + theme.Hint().Render(kit.ClipEllipsis(r.reason, max(inner-48, 10)))
		}
		on := r.on || st.allBots
		lines = append(lines, row(i+1, kit.Checkbox(label, on, st.cur == i+1)))
	}
	lines = append(lines, "", theme.Hint().Render("space toggle · r recommended · a all bots · n none · enter save · esc keep as is"))
	box := kit.Modal{Title: "give " + st.item.Name + " to…", Lines: lines, Width: inner + 6}
	return box.View()
}
