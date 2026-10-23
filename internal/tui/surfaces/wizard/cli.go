package wizard

import (
	"fmt"

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
// missing one (ADR-0044): the user's package manager installs it after
// the exact command is shown and confirmed; the rest show the vendor's
// docs.
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
		case s.installable(row.Name):
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

// installCmd is the command that installs name here ("" = none).
func (s *cliStep) installCmd(name string) string {
	if s.env.CLIInstall == nil || s.env.RunInstall == nil {
		return ""
	}
	cmd, ok := s.env.CLIInstall(name)
	if !ok {
		return ""
	}
	return cmd
}

func (s *cliStep) installable(name string) bool { return s.installCmd(name) != "" }

func (s *cliStep) start(row CLIRow) {
	s.phase = cliInstalling
	name := row.Name
	s.pending = s.env.RunInstall(name, func(err error) tea.Msg { return cliInstalledMsg{name: name, err: err} })
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
	for _, r := range s.rows {
		if r.Name == m.name && r.Version == "" {
			s.errText = "the install finished but " + m.name + " is still not on your PATH — check your shell profile, then press r"
			return nil
		}
	}
	s.env.Changed = true // the runtime registry reads CLIs at launch
	s.env.Applied = append(s.env.Applied, "installed "+m.name+" ("+s.installCmd(m.name)+")")
	s.msg = m.name + " installed"
	return nil
}

// status is a row's state as a badge (F-098): the glyph carries the
// meaning, the hue repeats it.
func (s *cliStep) status(r CLIRow) string {
	switch {
	case r.Version == "":
		return kit.Badge(theme.BadgeNeutral, "not installed")
	case r.Verdict == clirun.VerdictExact:
		return kit.Badge(theme.BadgeOK, "ready")
	case r.Verdict == clirun.VerdictDrift:
		return kit.Badge(theme.BadgeWarn, "ready · "+clirun.Relation(r.Tested, r.Version)+" verified "+r.Tested)
	default:
		return kit.Badge(theme.BadgeError, r.Why)
	}
}

// Keys names the step's own keys for the status row, by phase.
func (s *cliStep) Keys() []string {
	switch s.phase {
	case cliConfirm:
		return []string{"y install", "n cancel"}
	case cliInstalling:
		return nil
	case cliManualInfo:
		return []string{"r check again", "esc back"}
	}
	keys := []string{"↑/↓ select"}
	if row, ok := s.selected(); ok && row.Version == "" {
		if s.installable(row.Name) {
			keys = append(keys, "i install")
		} else {
			keys = append(keys, "i how to install")
		}
	}
	return append(keys, "r re-check")
}

func (s *cliStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("Coding CLIs — the engines your employees think with"),
		dim("DHI drives the CLI you already use; install one here if you have none."), "",
	}
	for i, r := range s.rows {
		mark, name := "  ", theme.TextStyle().Render(fmt.Sprintf("%-13s", r.Name))
		if i == s.cur {
			mark = theme.AccentText().Render(theme.GlyphCursor) + " "
			name = theme.AccentText().Render(fmt.Sprintf("%-13s", r.Name))
		}
		ver := dim(fmt.Sprintf("%-11s", orDash(r.Version)))
		out = append(out, kit.ClipEllipsis(mark+name+ver+s.status(r), w))
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
		out = append(out, "", theme.TextStyle().Render("DHI will run this with your package manager, in your terminal:"), "",
			"  "+theme.AccentText().Render(s.installCmd(row.Name)), "",
			dim("You see its output and answer any prompt; DHI comes back when it ends."),
			dim("Docs: "+orDash(row.Plan.Docs)))
		bl, _ := kit.Buttons([]kit.Button{{Key: "y", Label: "Install", Focused: true},
			{Key: "n", Label: "Cancel", Variant: theme.ButtonGhost}}, w)
		out = append(append(out, ""), bl...)
	case cliInstalling:
		out = append(out, "", kit.SpinnerGlyph(f)+" "+dim("installing "+row.Name+" in your terminal…"))
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
		out = append(out, "", dim("Then press r to check again."))
	default:
		if hasRow && row.Plan.Auth != "" {
			out = append(out, "")
			for _, l := range kit.WrapWords("sign in: "+row.Plan.Auth, w) {
				out = append(out, dim(l))
			}
		}
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
