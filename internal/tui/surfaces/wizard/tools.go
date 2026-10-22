package wizard

import (
	"fmt"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ToolRow is one host tool as the tools step shows it (F-114).
type ToolRow struct {
	Name    string
	Purpose string
	Version string // where it is installed ("" = not installed)
	Core    bool   // a whole feature needs it (git, rg, gh)
	Install string // the package-manager command, or where to get it
	CanRun  bool   // Install is a command DHI can run (after a yes)
}

type toolsInstalledMsg struct {
	name string
	err  error
}

// toolsStep lists the tools DHI uses from your system (ADR-0044) and
// installs a missing one with your package manager after you saw the
// exact command — the same flow as the coding-CLI step.
type toolsStep struct {
	env     *Env
	rows    []ToolRow
	cur     int
	confirm bool
	busy    bool
	msg     string
	errText string
	pending tea.Cmd
}

func (*toolsStep) ID() string    { return "tools" }
func (*toolsStep) Title() string { return "tools" }

// Applies while a core tool is missing; it can always run again.
func (s *toolsStep) Applies() bool {
	if s.env.ToolStatus == nil {
		return false
	}
	for _, r := range s.env.ToolStatus() {
		if r.Core && r.Version == "" {
			return true
		}
	}
	return false
}

func (s *toolsStep) CanRerun() bool { return s.env.ToolStatus != nil }

func (s *toolsStep) Enter() tea.Cmd {
	s.confirm, s.msg, s.errText = false, "", ""
	s.refresh()
	return nil
}

func (s *toolsStep) refresh() {
	s.rows = s.env.ToolStatus()
	if s.cur >= len(s.rows) {
		s.cur = max(len(s.rows)-1, 0)
	}
}

// Busy blocks wizard navigation while an install runs.
func (s *toolsStep) Busy() bool { return s.busy }

func (s *toolsStep) TakeCmd() tea.Cmd {
	c := s.pending
	s.pending = nil
	return c
}

func (s *toolsStep) HandleKey(key string) Action {
	if s.busy {
		return Stay
	}
	if s.confirm {
		switch key {
		case "y", "enter":
			s.confirm, s.busy = false, true
			name := s.rows[s.cur].Name
			s.pending = s.env.RunInstall(name, func(err error) tea.Msg { return toolsInstalledMsg{name: name, err: err} })
		case "n", "esc":
			s.confirm = false
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
		if s.cur < len(s.rows) {
			r := s.rows[s.cur]
			s.msg, s.errText = "", ""
			switch {
			case r.Version != "":
				s.msg = r.Name + " is already installed"
			case r.CanRun && s.env.RunInstall != nil:
				s.confirm = true
			default:
				s.msg = "install " + r.Name + " from " + r.Install + ", then press r"
			}
		}
	case "enter":
		return Next
	case "esc":
		return Skip
	}
	return Stay
}

func (s *toolsStep) Update(msg tea.Msg) tea.Cmd {
	m, ok := msg.(toolsInstalledMsg)
	if !ok {
		return nil
	}
	s.busy = false
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
	s.env.Applied = append(s.env.Applied, "installed "+m.name)
	s.msg = m.name + " installed"
	return nil
}

func (s *toolsStep) Apply() error { return nil }

func (s *toolsStep) Keys() []string {
	if s.confirm {
		return []string{"y run it", "n cancel"}
	}
	return []string{"↑/↓ select", "i install", "r re-check"}
}

func (s *toolsStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("Your tools — DHI uses what is on your system"),
		dim("Nothing is downloaded by DHI. A missing tool installs with your package manager when you say yes."), "",
	}
	for i, r := range s.rows {
		mark, name := "  ", theme.TextStyle().Render(fmt.Sprintf("%-28s", r.Name))
		if i == s.cur {
			mark = theme.AccentText().Render(theme.GlyphCursor) + " "
			name = theme.AccentText().Render(fmt.Sprintf("%-28s", r.Name))
		}
		status := kit.Badge(theme.BadgeOK, "installed")
		switch {
		case r.Version == "" && r.Core:
			status = kit.Badge(theme.BadgeWarn, "not installed")
		case r.Version == "":
			status = kit.Badge(theme.BadgeNeutral, "not installed")
		}
		out = append(out, kit.ClipEllipsis(mark+name+status, w))
	}
	if s.cur < len(s.rows) {
		r := s.rows[s.cur]
		out = append(out, "", dim(r.Name+" — "+r.Purpose))
		if r.Version != "" {
			out = append(out, dim(r.Version))
		}
	}
	if s.confirm {
		r := s.rows[s.cur]
		out = append(out, "", theme.TextStyle().Render("DHI will run this with your package manager, in your terminal:"), "",
			"  "+theme.AccentText().Render(r.Install), "",
			dim("You see its output and answer any prompt; DHI comes back when it ends."))
	}
	if s.busy {
		out = append(out, "", kit.SpinnerGlyph(f)+" "+dim("installing in your terminal…"))
	}
	if s.msg != "" {
		out = append(out, "", ok(s.msg))
	}
	if s.errText != "" {
		out = append(out, "", bad(s.errText))
	}
	return out
}
