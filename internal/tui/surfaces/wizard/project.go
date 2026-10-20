package wizard

import (
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// projectHelp says what each brief field is for (F-098): shown under the
// field being edited, so the form explains itself where you look.
var projectHelp = map[string]string{
	"name":    "What everyone calls it.",
	"summary": "One or two sentences: what it does and for whom.",
	"goals":   "What matters now — agents weigh trade-offs against these.",
	"users":   "Who uses it, so agents write for them.",
	"stack":   "Languages, frameworks, services. Agents pick tools from this.",
	"process": "How work flows from a task to a merge.",
	"review":  "What a change needs before it ships.",
	"testing": "What to run before saying done.",
	"dont":    "Hard lines agents must never cross — one per line.",
}

// projectStep drafts the project brief (F-080) from the repo and lets
// the user correct it; enter accepts the draft as is. Every field is a
// multi-line input: the list shows each field's first line, and the
// focused one opens below with its help (F-098).
type projectStep struct {
	base
	env    *Env
	inputs []kit.Input
	cur    int
	err    string
}

func (*projectStep) ID() string    { return "project" }
func (*projectStep) Title() string { return "project" }

// Applies once a workspace exists and has no brief yet.
func (s *projectStep) Applies() bool {
	return s.env.Root != "" && s.env.DetectBrief != nil && s.env.SaveBrief != nil &&
		(s.env.HasBrief == nil || !s.env.HasBrief())
}

func (s *projectStep) Enter() tea.Cmd {
	draft := s.env.DetectBrief()
	s.inputs = make([]kit.Input, len(brief.Fields))
	for i, f := range brief.Fields {
		s.inputs[i] = kit.NewInput(draft.Get(f.Key), "")
		s.inputs[i].Multiline = true
	}
	s.cur, s.err = 0, ""
	return nil
}

func (s *projectStep) HandleKey(key string) Action {
	switch key {
	case "esc":
		return Skip
	case "enter":
		return Next
	case "tab", "down":
		s.cur = (s.cur + 1) % len(s.inputs)
		return Stay
	case "shift+tab", "up":
		s.cur = (s.cur - 1 + len(s.inputs)) % len(s.inputs)
		return Stay
	}
	s.inputs[s.cur].HandleKey(key)
	return Stay
}

func (s *projectStep) CapturesText() bool { return true }

// Keys names the step's own keys for the status row.
func (s *projectStep) Keys() []string { return []string{"tab next field", "^j new line"} }

func (s *projectStep) Apply() error {
	var b brief.Brief
	for i, f := range brief.Fields {
		_ = b.Set(f.Key, strings.TrimSpace(s.inputs[i].Value))
	}
	if err := s.env.SaveBrief(b); err != nil {
		s.err = err.Error()
		return err
	}
	s.env.Applied = append(s.env.Applied, "project brief saved — every employee reads it")
	return nil
}

func (s *projectStep) View(w, f int) []string {
	out := []string{theme.TextStyle().Render("Tell your team about the project.")}
	for _, l := range kit.WrapWords("We drafted this from your repo. Fix anything that's off, or press enter to keep it. "+
		"Every agent reads it on every task; change it any time in Settings → PROJECT.", w) {
		out = append(out, dim(l))
	}
	out = append(out, "")
	const labelW = 10
	for i, fd := range brief.Fields {
		first, more, _ := strings.Cut(s.inputs[i].Value, "\n")
		val := first
		if more != "" {
			val += "  +" + strconv.Itoa(strings.Count(more, "\n")+1) + " more"
		}
		if val == "" {
			val = "—"
		}
		mark, labelSt, valSt := "  ", theme.TextDim(), theme.TextDim()
		if i == s.cur {
			mark, labelSt, valSt = theme.AccentText().Render(theme.GlyphCursor)+" ", theme.AccentText(), theme.TextStyle()
		}
		out = append(out, mark+labelSt.Render(padRight(fd.Label, labelW))+valSt.Render(kit.ClipEllipsis(val, w-labelW-2)))
	}
	fd := brief.Fields[s.cur]
	in := s.inputs[s.cur]
	in.Focused = true
	in.Help = projectHelp[fd.Key]
	in.Err = s.err
	out = append(out, "", theme.SectionHeader().Render(strings.ToUpper(fd.Title)))
	return append(out, in.View(w)...)
}

// padRight pads s with spaces to n cells.
func padRight(s string, n int) string {
	if r := []rune(s); len(r) < n {
		return s + strings.Repeat(" ", n-len(r))
	}
	return s
}
