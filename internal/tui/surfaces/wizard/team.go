package wizard

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/starter"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// engineLater is the engine toggle's "leave it unset" choice.
const engineLater = "decide later"

// enginePreference orders detected CLIs for the default: the first one
// installed wins unless the user already configured an engine. Names not
// listed sort after, alphabetically.
var enginePreference = []string{"claude", "codex", "opencode", "cursor-agent", "copilot", "antigravity"}

func orderEngines(names []string) []string {
	rank := func(n string) int {
		for i, p := range enginePreference {
			if p == n {
				return i
			}
		}
		return len(enginePreference)
	}
	sort.SliceStable(names, func(i, j int) bool {
		ri, rj := rank(names[i]), rank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	return names
}

// custom is the team toggle's "pick roles and counts" choice (F-079).
const custom = "custom"

// Field order in the team form: the team choice, the engine, then one
// count per starter.Slots role (shown only for custom).
const (
	fieldTeam = iota
	fieldEngine
	fieldCounts
)

var countChoices = []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

// teamStep offers a team to a workspace that has no employees yet:
// role counts (F-079) or a starter team (F-045), and picks the default
// engine from the CLIs found.
type teamStep struct {
	base
	env       *Env
	templates []starter.Template
	form      *kit.Form
	engines   []string
}

func (*teamStep) ID() string    { return "team" }
func (*teamStep) Title() string { return "team" }

// Applies only to an existing workspace with an empty roster, so a
// re-run never offers to duplicate an existing team.
func (s *teamStep) Applies() bool {
	return s.env.Root != "" && (s.env.ApplyTeam != nil || s.env.ComposeTeam != nil) &&
		(s.env.RosterCount == nil || s.env.RosterCount() == 0)
}

func (s *teamStep) Enter() tea.Cmd {
	s.templates = starter.Templates()
	var slugs []string
	if s.env.ComposeTeam != nil {
		slugs = append(slugs, custom)
	}
	pickTeam := 0
	if s.env.ApplyTeam != nil {
		for _, t := range s.templates {
			if t.Slug == "squad" && s.env.ComposeTeam == nil {
				pickTeam = len(slugs)
			}
			slugs = append(slugs, t.Slug)
		}
	}
	s.engines = s.engines[:0]
	if s.env.DetectCLIs != nil {
		for name, ver := range s.env.DetectCLIs() {
			if ver != "" {
				s.engines = append(s.engines, name)
			}
		}
		orderEngines(s.engines)
	}
	choices := append(append([]string(nil), s.engines...), engineLater)
	pick := 0
	for i, e := range s.engines { // keep an engine the user already configured
		if "cli:"+e == s.env.Engine {
			pick = i
		}
	}
	fields := []kit.Field{
		kit.NewToggleField("team", slugs, pickTeam),
		kit.NewToggleField("engine", choices, pick),
	}
	if s.env.ComposeTeam != nil {
		def := starter.DefaultCounts()
		for _, sl := range starter.Slots {
			fields = append(fields, kit.NewToggleField(sl.Label, countChoices, def[sl.Role]))
		}
	}
	s.form = kit.NewForm("team", fields...)
	return nil
}

func (s *teamStep) HandleKey(key string) Action {
	switch s.form.HandleKey(key) {
	case kit.FormCancel:
		return Skip
	case kit.FormSubmit:
		return Next
	}
	// Count rows only matter for custom: tab skips them otherwise.
	if !s.isCustom() && s.form.Cur() >= fieldCounts && (key == "tab" || key == "shift+tab") {
		for s.form.Cur() != fieldTeam {
			s.form.HandleKey(key)
		}
	}
	return Stay
}

func (s *teamStep) CapturesText() bool { return false }

func (s *teamStep) isCustom() bool { return s.form.Values()[fieldTeam] == custom }

// counts reads the role counts from the form.
func (s *teamStep) counts() starter.Counts {
	c := starter.Counts{}
	v := s.form.Values()
	for i, sl := range starter.Slots {
		if fieldCounts+i < len(v) {
			if n, err := strconv.Atoi(v[fieldCounts+i]); err == nil && n > 0 {
				c[sl.Role] = n
			}
		}
	}
	return c
}

func (s *teamStep) chosen() starter.Template {
	slug := s.form.Values()[fieldTeam]
	for _, t := range s.templates {
		if t.Slug == slug {
			return t
		}
	}
	return starter.Template{}
}

func (s *teamStep) Apply() error {
	if s.isCustom() {
		res, err := s.env.ComposeTeam(s.counts())
		if err != nil {
			s.form.SetError(err.Error())
			return err
		}
		s.env.Changed = true
		for _, r := range res {
			s.env.Applied = append(s.env.Applied,
				fmt.Sprintf("team %s created: %s", r.Team, strings.Join(r.Created, ", ")))
		}
	} else {
		tpl := s.chosen()
		res, err := s.env.ApplyTeam(tpl.Slug)
		if err != nil {
			s.form.SetError(err.Error())
			return err
		}
		s.env.Changed = true
		s.env.Applied = append(s.env.Applied,
			fmt.Sprintf("team %s created: %s", res.Team, strings.Join(res.Created, ", ")))
	}
	if engine := s.form.Values()[fieldEngine]; engine != engineLater && s.env.SetEngine != nil {
		if err := s.env.SetEngine("cli:" + engine); err != nil {
			// The team exists now; keep the step open on the engine only.
			s.form.SetError("team created, but the default engine was not saved: " + err.Error())
			return err
		}
		if line := "default engine cli:" + engine; !slices.Contains(s.env.Applied, line) {
			s.env.Applied = append(s.env.Applied, line) // the CLI step may have said it already
		}
	}
	return nil
}

func (s *teamStep) View(w, f int) []string {
	out := []string{theme.TextStyle().Render("Who's on your team?")}
	for _, l := range kit.WrapWords("Pick how many of each role you want, or a ready-made team. Change it any time in Settings → TEAMS.", w) {
		out = append(out, dim(l))
	}
	out = append(out, "")
	lines := formLines(s.form)
	// The count rows are hidden unless custom is chosen; custom shows
	// them as a role grid with −/+ steppers (F-093) instead of one
	// "[n]" toggle per row.
	rest := append([]string{}, lines[fieldCounts+len(starter.Slots):]...)
	out = append(out, lines[:fieldCounts]...)
	if s.isCustom() {
		out = append(out, s.roleGrid(w)...)
	}
	out = append(out, rest...)
	if len(s.engines) == 0 {
		out = append(out, "", warn("No coding CLI found on your PATH."),
			dim("Employees will wait until you install one and pick it in Settings."))
	} else {
		for _, l := range kit.WrapWords("coding CLIs found: "+strings.Join(s.engines, ", ")+" — ←/→ picks the default engine", w) {
			out = append(out, dim(l)) // many CLIs wrap instead of clipping
		}
	}
	if s.isCustom() {
		return append(out, s.customPreview()...)
	}
	tpl := s.chosen()
	out = append(out, "", theme.AccentText().Render(tpl.Name))
	for _, l := range kit.WrapWords(tpl.Description, w) {
		out = append(out, dim(l))
	}
	for _, e := range tpl.Employees {
		lead := ""
		if e.ID == tpl.Lead {
			lead = theme.WarningText().Render("  ★ lead")
		}
		out = append(out, fmt.Sprintf("  %s %s %s%s",
			theme.TextStyle().Render(fmt.Sprintf("%-8s", e.Name)),
			dim(fmt.Sprintf("%-9s", e.Role)),
			dim(e.Persona), lead))
	}
	if tpl.Lead == "you" {
		out = append(out, dim("  you lead this team"))
	}
	return out
}

// roleGrid renders the role counts as cells, two per row when wide:
// "leads  − 1 +". The focused cell carries the cursor and the view's
// hue; zero counts stay muted so the chosen roles stand out. ←/→ (the
// form's cycle keys) step the focused count.
func (s *teamStep) roleGrid(w int) []string {
	const cellW = 26
	cols := 1
	if w >= cellW*2+2 {
		cols = 2
	}
	vals := s.form.Values()
	cur := s.form.Cur()
	var out []string
	row := ""
	for i := range starter.Slots {
		f := fieldCounts + i
		label := s.form.Fields[f].Label
		v := vals[f]
		mark := " "
		lab := dim(fmt.Sprintf("%-10s", label))
		num := theme.TextMuted().Render(fmt.Sprintf("%2s", v))
		if v != "0" {
			num = theme.TextStyle().Bold(true).Render(fmt.Sprintf("%2s", v))
		}
		step := func(g string) string { return theme.TextMuted().Render(g) }
		if f == cur {
			mark = theme.GlyphCursor
			lab = theme.RailActive().Render(fmt.Sprintf("%-10s", label))
			step = func(g string) string { return theme.AccentText().Render(g) }
		}
		cell := lab + mark + step("−") + " " + num + " " + step("+")
		cell += strings.Repeat(" ", max(cellW-ansi.Width(cell), 0))
		row += cell
		if (i+1)%cols == 0 {
			out = append(out, strings.TrimRight(row, " "))
			row = ""
		}
	}
	if row != "" {
		out = append(out, row)
	}
	return out
}

// customPreview shows the teams the counts would create, one line each.
func (s *teamStep) customPreview() []string {
	c := s.counts()
	out := []string{"", theme.AccentText().Render(c.Summary())}
	tpls, err := starter.Compose(c, starter.Options{})
	if err != nil {
		return append(out, warn(err.Error()))
	}
	for _, t := range tpls {
		var who []string
		for _, e := range t.Employees {
			if e.ID == t.Lead {
				who = append(who, e.Name+" ★")
				continue
			}
			who = append(who, e.Name+" ("+e.Role+")")
		}
		line := "  " + theme.TextStyle().Render(t.Team) + dim("  "+strings.Join(who, ", "))
		if t.Lead == "you" {
			line += dim("  · you lead")
		}
		out = append(out, line)
	}
	return out
}
