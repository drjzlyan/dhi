package wizard

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// projectStep drafts the project brief (F-080) from the repo and lets
// the user correct it; enter accepts the draft as is.
type projectStep struct {
	base
	env  *Env
	form *kit.Form
}

func (*projectStep) ID() string    { return "project" }
func (*projectStep) Title() string { return "project" }

// Applies once a workspace exists and has no brief yet.
func (s *projectStep) Applies() bool {
	return s.env.Root != "" && s.env.DetectBrief != nil && s.env.SaveBrief != nil &&
		(s.env.HasBrief == nil || !s.env.HasBrief())
}

// escapeNL shows a multi-line value on one form row; unescapeNL undoes it.
func escapeNL(v string) string   { return strings.ReplaceAll(v, "\n", `\n`) }
func unescapeNL(v string) string { return strings.ReplaceAll(strings.TrimSpace(v), `\n`, "\n") }

func (s *projectStep) Enter() tea.Cmd {
	draft := s.env.DetectBrief()
	fields := make([]kit.Field, len(brief.Fields))
	for i, f := range brief.Fields {
		fields[i] = kit.NewTextField(f.Label, escapeNL(draft.Get(f.Key)))
	}
	s.form = kit.NewForm("project", fields...)
	return nil
}

func (s *projectStep) HandleKey(key string) Action {
	switch s.form.HandleKey(key) {
	case kit.FormCancel:
		return Skip
	case kit.FormSubmit:
		return Next
	}
	return Stay
}

func (s *projectStep) CapturesText() bool { return true }

func (s *projectStep) Apply() error {
	var b brief.Brief
	for i, f := range brief.Fields {
		_ = b.Set(f.Key, unescapeNL(s.form.Values()[i]))
	}
	if err := s.env.SaveBrief(b); err != nil {
		s.form.SetError(err.Error())
		return err
	}
	s.env.Applied = append(s.env.Applied, "project brief saved — every employee reads it")
	return nil
}

func (s *projectStep) View(w, f int) []string {
	out := []string{theme.TextStyle().Render("Tell your team about the project.")}
	for _, l := range kit.WrapWords("We drafted this from your repo. Fix anything that's off, add how you like to work, or press enter to keep it. "+
		"Your employees read it on every task; change it any time in Settings → PROJECT.", w) {
		out = append(out, dim(l))
	}
	out = append(out, "")
	l := s.form.ViewWidth(w)
	out = append(out, l[:len(l)-1]...) // the wizard has its own hint
	return append(out, "", dim(`\n starts a new line · tab next field`))
}
