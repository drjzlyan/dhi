package wizard

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/starter"
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

// teamStep offers a starter team (F-045) to a workspace that has no
// employees yet, and picks the default engine from the CLIs found.
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
	return s.env.Root != "" && s.env.ApplyTeam != nil &&
		(s.env.RosterCount == nil || s.env.RosterCount() == 0)
}

func (s *teamStep) Enter() tea.Cmd {
	s.templates = starter.Templates()
	slugs := make([]string, len(s.templates))
	squad := 0
	for i, t := range s.templates {
		slugs[i] = t.Slug
		if t.Slug == "squad" {
			squad = i
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
	s.form = kit.NewForm("team",
		kit.NewToggleField("team", slugs, squad),
		kit.NewToggleField("engine", choices, pick),
	)
	return nil
}

func (s *teamStep) HandleKey(key string) Action {
	switch s.form.HandleKey(key) {
	case kit.FormCancel:
		return Skip
	case kit.FormSubmit:
		return Next
	}
	return Stay
}

func (s *teamStep) CapturesText() bool { return false }

func (s *teamStep) chosen() starter.Template {
	slug := s.form.Values()[0]
	for _, t := range s.templates {
		if t.Slug == slug {
			return t
		}
	}
	return starter.Template{}
}

func (s *teamStep) Apply() error {
	tpl := s.chosen()
	res, err := s.env.ApplyTeam(tpl.Slug)
	if err != nil {
		s.form.SetError(err.Error())
		return err
	}
	s.env.Changed = true
	s.env.Applied = append(s.env.Applied,
		fmt.Sprintf("team %s created: %s", res.Team, strings.Join(res.Created, ", ")))
	if engine := s.form.Values()[1]; engine != engineLater && s.env.SetEngine != nil {
		if err := s.env.SetEngine("cli:" + engine); err != nil {
			// The team exists now; keep the step open on the engine only.
			s.form.SetError("team created, but the default engine was not saved: " + err.Error())
			return err
		}
		s.env.Applied = append(s.env.Applied, "default engine cli:"+engine)
	}
	return nil
}

func (s *teamStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("Who's on your team?"),
		dim("A starter team gives you working employees now; edit or replace any of them later."), "",
	}
	out = append(out, formLines(s.form)...)
	if len(s.engines) == 0 {
		out = append(out, "", warn("No coding CLI found on your PATH."),
			dim("Employees will wait until you install one and pick it in Settings."))
	} else {
		out = append(out, dim("coding CLIs found: "+strings.Join(s.engines, ", ")+" — ←/→ picks the default engine"))
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
