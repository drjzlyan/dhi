package wizard

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// CLIRow is one coding CLI as the wizard shows it.
type CLIRow struct {
	Name    string
	Version string // "" = not installed
	Verdict clirun.Verdict
	Why     string // clirun.Assess's explanation for an installed CLI
	Tested  string // the version the adapter was verified against
	Plan    clirun.InstallPlan
}

type cliPhase uint8

const (
	cliList cliPhase = iota
	cliConfirm
	cliInstalling
	cliManualInfo
)

type cliInstalledMsg struct {
	name string
	err  error
}

// cliStep lets the user see which coding CLIs are ready and install a
// missing one (ADR-0027): npm-distributed CLIs install after the exact
// command is shown and confirmed; the rest show the vendor's command.
type cliStep struct {
	env     *Env
	rows    []CLIRow
	cur     int
	phase   cliPhase
	msg     string // last result, success
	errText string // last failure
	pending tea.Cmd
}

func (*cliStep) ID() string    { return "cli" }
func (*cliStep) Title() string { return "coding CLI" }

func (s *cliStep) Applies() bool { return s.env.CLIStatus != nil }

func (s *cliStep) Enter() tea.Cmd {
	s.phase, s.msg, s.errText = cliList, "", ""
	s.refresh()
	return nil
}

// Busy blocks wizard navigation while an install runs.
func (s *cliStep) Busy() bool { return s.phase == cliInstalling }

func (s *cliStep) TakeCmd() tea.Cmd {
	c := s.pending
	s.pending = nil
	return c
}

func (s *cliStep) refresh() {
	s.rows = s.env.CLIStatus()
	if s.cur >= len(s.rows) {
		s.cur = max(len(s.rows)-1, 0)
	}
}

func (s *cliStep) selected() (CLIRow, bool) {
	if s.cur < 0 || s.cur >= len(s.rows) {
		return CLIRow{}, false
	}
	return s.rows[s.cur], true
}

func (s *cliStep) HandleKey(key string) Action {
	row, ok := s.selected()
	switch s.phase {
	case cliInstalling:
		return Stay
	case cliConfirm:
		switch key {
		case "enter", "y":
			s.start(row)
		case "esc", "n":
			s.phase = cliList
		}
		return Stay
	case cliManualInfo:
		if key == "enter" || key == "esc" {
			s.phase = cliList
		}
		return Stay
	}
	switch key {
	case "j", "down":
		if s.cur < len(s.rows)-1 {
			s.cur++
		}
	case "k", "up":
		if s.cur > 0 {
			s.cur--
		}
	case "r":
		s.msg, s.errText = "", ""
		s.refresh()
	case "i":
		if !ok {
			return Stay
		}
		s.msg, s.errText = "", ""
		switch {
		case row.Version != "":
			s.msg = row.Name + " is already installed"
		case row.Plan.Method == clirun.MethodNPM && s.env.InstallCLI != nil:
			s.phase = cliConfirm
		default:
			s.phase = cliManualInfo
		}
	case "enter":
		return Next
	case "esc":
		return Skip
	}
	return Stay
}

// usable reports a CLI that is installed and not in a different major
// version than DHI was verified against.
func usable(r CLIRow) bool {
	return r.Version != "" && r.Verdict != clirun.VerdictMajor && r.Verdict != clirun.VerdictUnknown
}

// defaultEngine is the engine this step will set when the workspace has
// none: the most preferred usable CLI. "" = nothing to set (an engine is
// already configured, there is no workspace, or no CLI is usable).
func (s *cliStep) defaultEngine() string {
	if s.env.Engine != "" || s.env.Root == "" || s.env.SetEngine == nil {
		return ""
	}
	var ready []string
	for _, r := range s.rows {
		if usable(r) {
			ready = append(ready, r.Name)
		}
	}
	if len(ready) == 0 {
		return ""
	}
	return "cli:" + orderEngines(ready)[0]
}

// Apply gives a workspace without a default engine one, so employees on a
// freshly cloned team workspace can think without anyone hand-editing a
// config (the team step only runs for an empty roster).
func (s *cliStep) Apply() error {
	engine := s.defaultEngine()
	if engine == "" {
		return nil
	}
	if err := s.env.SetEngine(engine); err != nil {
		s.errText = "could not save the default engine: " + err.Error()
		return err
	}
	s.env.Engine = engine
	s.env.Changed = true
	s.env.Applied = append(s.env.Applied, "default engine "+engine)
	return nil
}

func (s *cliStep) start(row CLIRow) {
	s.phase = cliInstalling
	install, name := s.env.InstallCLI, row.Name
	s.pending = func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		return cliInstalledMsg{name: name, err: install(ctx, name)}
	}
}

func (s *cliStep) Update(msg tea.Msg) tea.Cmd {
	m, ok := msg.(cliInstalledMsg)
	if !ok {
		return nil
	}
	s.phase = cliList
	if m.err != nil {
		s.errText = fmt.Sprintf("installing %s failed: %v", m.name, m.err)
		return nil
	}
	s.refresh()
	s.env.Changed = true // the runtime registry reads CLIs at launch
	plan := clirun.InstallPlan{}
	for _, r := range s.rows {
		if r.Name == m.name {
			plan = r.Plan
		}
	}
	s.env.Applied = append(s.env.Applied, "installed "+m.name+" ("+plan.Package+")")
	s.msg = m.name + " installed"
	return nil
}

func (s *cliStep) status(r CLIRow) string {
	switch {
	case r.Version == "":
		if r.Plan.Method == clirun.MethodNPM && s.env.InstallCLI != nil {
			return dim("not installed · i installs it")
		}
		return dim("not installed · i shows how")
	case r.Verdict == clirun.VerdictExact:
		return ok("ready")
	case r.Verdict == clirun.VerdictDrift:
		return ok("ready") + " " + warn("("+clirun.Relation(r.Tested, r.Version)+" verified "+r.Tested+")")
	default:
		return bad(r.Why)
	}
}

func (s *cliStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("Coding CLIs — the engines your employees think with"),
		dim("DHI drives the CLI you already use; install one here if you have none."), "",
	}
	for i, r := range s.rows {
		mark, name := "  ", theme.TextStyle().Render(fmt.Sprintf("%-13s", r.Name))
		if i == s.cur {
			mark = theme.GlyphCursor + " "
			name = theme.TabActive().Render(fmt.Sprintf("%-13s", r.Name))
		}
		ver := dim(fmt.Sprintf("%-11s", orDash(r.Version)))
		out = append(out, mark+name+ver+s.status(r))
	}
	readyAny := false
	for _, r := range s.rows {
		if usable(r) {
			readyAny = true
		}
	}
	if !readyAny {
		out = append(out, "", warn("No CLI is ready yet — employees can't think until one is."))
	} else if e := s.defaultEngine(); e != "" {
		out = append(out, "", dim("default engine for this workspace: "+e+" (change it later in Settings)"))
	}

	row, hasRow := s.selected()
	switch s.phase {
	case cliConfirm:
		out = append(out, "", theme.TextStyle().Render("DHI will run, with its own npm (no sudo, nothing global):"), "",
			"  "+theme.AccentText().Render("npm install --prefix "+s.prefixOf(row.Name)+" "+row.Plan.Package), "",
			dim("Installs into DHI's folder only. Source: "+orDash(row.Plan.Docs)),
			dim("enter / y to install · esc / n to cancel"))
	case cliInstalling:
		out = append(out, "", kit.SpinnerGlyph(f)+" "+dim("installing "+row.Plan.Package+" with DHI's npm — this can take a minute…"))
	case cliManualInfo:
		out = append(out, "", theme.TextStyle().Render("Install "+row.Name+" yourself:"))
		if row.Plan.Command != "" {
			out = append(out, "", "  "+theme.AccentText().Render(row.Plan.Command))
		}
		if row.Plan.Docs != "" {
			out = append(out, dim("  docs: "+row.Plan.Docs))
		}
		if row.Plan.Note != "" {
			out = append(out, "", dim(row.Plan.Note))
		}
		out = append(out, "", dim("then press r to check again · esc to go back"))
	default:
		if hasRow && row.Plan.Auth != "" {
			out = append(out, "", dim("sign in: "+row.Plan.Auth))
		}
		out = append(out, "", dim("↑/↓ select · i install · r re-check · enter continue"))
	}
	if s.msg != "" {
		out = append(out, "", ok(s.msg))
	}
	if s.errText != "" {
		out = append(out, "", bad(s.errText))
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// prefixOf is where a managed install of name lands, for display.
func (s *cliStep) prefixOf(name string) string {
	if s.env.CLIPrefix != nil {
		return s.env.CLIPrefix(name)
	}
	return "<dhi toolchain>/" + clirun.ManagedSubdir + "/" + name
}
