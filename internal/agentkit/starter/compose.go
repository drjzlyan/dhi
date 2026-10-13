package starter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// Slot is one role a team can be composed from (F-079).
type Slot struct {
	Role  string // library role id
	Label string // what the user sees; fits a 10-column form label
}

// Slots lists the composable roles in display order; the lead comes
// first because each lead becomes its own team.
var Slots = []Slot{
	{"lead", "leads"},
	{"fullstack", "fullstack"},
	{"frontend", "frontend"},
	{"backend", "backend"},
	{"tester", "testers"},
	{"product-manager", "PMs"},
	{"designer", "designers"},
	{"devops", "devops"},
}

// MaxPerRole caps one role's count; MaxEmployees caps a whole compose.
const (
	MaxPerRole   = 9
	MaxEmployees = 40
)

// DefaultCounts is the composition the wizard offers first.
func DefaultCounts() Counts {
	return Counts{"lead": 1, "fullstack": 2, "tester": 1}
}

// Counts maps a role id to how many employees to create.
type Counts map[string]int

// Total is the number of employees the counts ask for.
func (c Counts) Total() int {
	n := 0
	for _, s := range Slots {
		n += c[s.Role]
	}
	return n
}

// Teams is how many teams the counts create: one per lead, or one led
// by the human when there is no lead.
func (c Counts) Teams() int {
	if c["lead"] > 0 {
		return c["lead"]
	}
	if c.Total() > 0 {
		return 1
	}
	return 0
}

// Summary is the one-line description the forms show.
func (c Counts) Summary() string {
	t, e := c.Teams(), c.Total()
	return fmt.Sprintf("%d %s · %d %s", t, plural(t, "team", "teams"), e, plural(e, "employee", "employees"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Options steers Compose.
type Options struct {
	// AgentTaken / TeamTaken report ids and team slugs already in use
	// (including archived agents); nil means none.
	AgentTaken func(id string) bool
	TeamTaken  func(slug string) bool
	// Into adds every employee to this existing team instead of
	// creating new ones; leads are refused in this mode.
	Into string
	// Persona, when set, overrides the per-role persona rotation (the
	// principal's reading style, F-081).
	Persona string
}

// names is the pool employee names are drawn from, in order.
var names = []string{
	"Atlas", "Forge", "Sage", "Quill", "Scout", "Anvil", "Nova", "Orion",
	"Iris", "Juno", "Kai", "Lyra", "Milo", "Nia", "Otto", "Pax",
	"Rhea", "Sol", "Tess", "Uma", "Vega", "Wren", "Xan", "Yara",
	"Zed", "Ada", "Bram", "Cleo", "Dax", "Echo", "Finn", "Gaia",
	"Hugo", "Ines", "Jett", "Kira", "Leo", "Mira", "Nico", "Opal",
	"Piper", "Quinn", "Remy", "Skye", "Theo", "Uri", "Vale", "Wade",
}

// rolePersonas is the persona rotation per role; the first fits the role.
var rolePersonas = map[string][]string{
	"lead":            {"mentor", "pragmatic"},
	"tester":          {"meticulous", "terse"},
	"product-manager": {"friendly", "mentor"},
	"designer":        {"friendly", "meticulous"},
}

var defaultPersonas = []string{"pragmatic", "terse", "meticulous", "friendly"}

// PersonaFor is the persona the n-th (0-based) employee of role gets.
func PersonaFor(role string, n int) string {
	p := rolePersonas[role]
	if p == nil {
		p = defaultPersonas
	}
	return p[n%len(p)]
}

var roleSkills = map[string][]string{
	"lead":            {"code-review"},
	"tester":          {"test-writing"},
	"product-manager": {"docs"},
}

// Compose turns role counts into templates, one per team (F-079). Every
// non-lead employee is dealt round-robin across the leads' teams; with
// no lead there is one team led by the human. Ids skip anything taken.
func Compose(c Counts, opt Options) ([]Template, error) {
	for role, n := range c {
		if !knownSlot(role) {
			return nil, fmt.Errorf("compose: unknown role %q", role)
		}
		if n < 0 || n > MaxPerRole {
			return nil, fmt.Errorf("compose: %s count %d is outside 0–%d", role, n, MaxPerRole)
		}
	}
	total := c.Total()
	if total == 0 {
		return nil, fmt.Errorf("compose: pick at least one employee")
	}
	if total > MaxEmployees {
		return nil, fmt.Errorf("compose: %d employees is more than %d", total, MaxEmployees)
	}
	if opt.Into != "" && c["lead"] > 0 {
		return nil, fmt.Errorf("compose: leads start their own team — add them as a new team")
	}
	agentTaken := opt.AgentTaken
	if agentTaken == nil {
		agentTaken = func(string) bool { return false }
	}
	teamTaken := opt.TeamTaken
	if teamTaken == nil {
		teamTaken = func(string) bool { return false }
	}

	used := map[string]bool{}
	next := 0
	newEmployee := func(role string, n int) Employee {
		var name, id string
		for {
			base := names[next%len(names)]
			if round := next / len(names); round > 0 {
				base += strconv.Itoa(round + 1)
			}
			next++
			id = strings.ToLower(base)
			if !used[id] && !agentTaken(id) {
				name = base
				break
			}
		}
		used[id] = true
		persona := PersonaFor(role, n)
		if opt.Persona != "" {
			persona = opt.Persona
		}
		return Employee{ID: id, Name: name, Role: role, Persona: persona,
			Skills: append([]string(nil), roleSkills[role]...)}
	}

	var tpls []Template
	if leads := c["lead"]; leads > 0 {
		for i := 0; i < leads; i++ {
			lead := newEmployee("lead", i)
			tpls = append(tpls, Template{Lead: lead.ID, Employees: []Employee{lead}})
		}
	} else {
		tpls = []Template{{Lead: org.Human}}
	}
	k := 0
	for _, s := range Slots[1:] {
		for i := 0; i < c[s.Role]; i++ {
			t := &tpls[k%len(tpls)]
			t.Employees = append(t.Employees, newEmployee(s.Role, i))
			k++
		}
	}

	usedTeams := map[string]bool{}
	for i := range tpls {
		t := &tpls[i]
		switch {
		case opt.Into != "":
			t.Team = opt.Into
		case t.Lead == org.Human:
			t.Team = freeSlug("team", func(s string) bool { return usedTeams[s] || teamTaken(s) })
		default:
			t.Team = freeSlug(t.Lead+"-team", func(s string) bool { return usedTeams[s] || teamTaken(s) })
		}
		usedTeams[t.Team] = true
		t.Slug = "custom"
		t.Name = t.Team
		t.Description = fmt.Sprintf("%d employees", len(t.Employees))
	}
	return tpls, nil
}

func knownSlot(role string) bool {
	for _, s := range Slots {
		if s.Role == role {
			return true
		}
	}
	return false
}

func freeSlug(base string, taken func(string) bool) string {
	if !taken(base) {
		return base
	}
	for i := 2; ; i++ {
		if s := base + "-" + strconv.Itoa(i); !taken(s) {
			return s
		}
	}
}

// ApplyAll applies composed templates through Apply, all or nothing: if
// a later team fails, the agents and teams written before it are
// removed and an existing team gets its old membership back. A template
// whose team already exists (Options.Into) has its employees added to it.
func ApplyAll(ws *workspace.Workspace, o *org.Org, lib *library.Store, tpls []Template) ([]Result, error) {
	type undo struct {
		res  Result
		prev *org.Team
	}
	var done []undo
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			d := done[i]
			if d.prev != nil {
				_ = o.UpdateTeam(d.prev.Name, d.prev.Lead, d.prev.Members)
			} else if d.res.TeamCreated {
				_ = o.DeleteTeam(d.res.Team)
			}
			for _, id := range d.res.Created {
				_ = o.DeleteAgent(ws, id)
			}
		}
	}
	var out []Result
	for _, t := range tpls {
		var prev *org.Team
		if tm, ok := o.Team(t.Team); ok {
			prev = &tm
		}
		res, err := Apply(ws, o, lib, t)
		if err != nil {
			rollback()
			return nil, err
		}
		done = append(done, undo{res: res, prev: prev})
		if prev != nil && len(res.Created) > 0 {
			members := append(append([]string(nil), prev.Members...), res.Created...)
			if err := o.UpdateTeam(t.Team, prev.Lead, members); err != nil {
				rollback()
				return nil, fmt.Errorf("starter: add to team %s: %w", t.Team, err)
			}
		}
		out = append(out, res)
	}
	return out, nil
}
