package wizard

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

var instructionScopes = []string{"every workspace", "this workspace"}

// instructionsStep asks what every employee should always do (F-090):
// saved for every workspace by default, so a new workspace needs no
// setup. Enter on an empty form skips.
type instructionsStep struct {
	base
	env  *Env
	form *kit.Form
}

func (*instructionsStep) ID() string    { return "instructions" }
func (*instructionsStep) Title() string { return "instructions" }

// Applies until you have instructions of your own: those already hold
// in every workspace, so a new workspace does not ask again.
func (s *instructionsStep) Applies() bool {
	if s.env.Instructions == nil || s.env.SaveInstructions == nil {
		return false
	}
	mine, _ := s.env.Instructions()
	return len(mine) == 0
}

func (s *instructionsStep) Enter() tea.Cmd {
	mine, here := s.env.Instructions()
	at, lines := 0, mine
	if len(mine) == 0 && len(here) > 0 && s.env.Root != "" {
		at, lines = 1, here
	}
	scopes := instructionScopes
	if s.env.Root == "" {
		scopes = instructionScopes[:1] // no workspace yet: only yours
	}
	s.form = kit.NewForm("instructions",
		kit.NewTextField("always", strings.Join(lines, "; ")),
		kit.NewToggleField("for", scopes, at))
	return nil
}

func (s *instructionsStep) HandleKey(key string) Action {
	switch s.form.HandleKey(key) {
	case kit.FormCancel:
		return Skip
	case kit.FormSubmit:
		return Next
	}
	return Stay
}

func (s *instructionsStep) CapturesText() bool { return true }

func (s *instructionsStep) Apply() error {
	var lines []string
	for _, l := range strings.Split(s.form.Values()[0], ";") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	everywhere := s.form.Values()[1] == instructionScopes[0]
	if err := s.env.SaveInstructions(lines, everywhere); err != nil {
		s.form.SetError(err.Error())
		return err
	}
	where := "this workspace"
	if everywhere {
		where = "every workspace"
	}
	s.env.Applied = append(s.env.Applied, "instructions for every employee saved for "+where)
	return nil
}

func (s *instructionsStep) View(w, f int) []string {
	out := []string{theme.TextStyle().Render("What should every employee always do?")}
	for _, l := range kit.WrapWords("A few house rules, separated by ; — e.g. \"write tests first; British English; never touch infra/\". "+
		"Saved for every workspace, so you set them once. Change them any time in Settings → STANDARDS. Enter with nothing to skip.", w) {
		out = append(out, dim(l))
	}
	out = append(out, "")
	l := s.form.ViewWidth(w)
	return append(out, l[:len(l)-1]...)
}
