package wizard

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/principal"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// styleStep asks who the user is and how they write and read (F-081).
// Every field starts on a sensible default, so enter is enough.
type styleStep struct {
	base
	env  *Env
	form *kit.Form
}

func (*styleStep) ID() string    { return "you" }
func (*styleStep) Title() string { return "you" }

// Applies when the profile can be saved; a re-run shows it again with
// the saved choices, which is how it is changed from the palette.
// Applies until your profile is saved; it then holds for every workspace
// and changes in Settings → CONFIG (F-090).
func (s *styleStep) Applies() bool { return s.env.SaveProfile != nil && !s.env.ProfileSaved }

var yesNo = []string{"no", "yes"}

// choicesWith keeps a hand-set value selectable.
func choicesWith(list []string, v string) ([]string, int) {
	if v == "" {
		return list, 0
	}
	if i := indexOf(list, v); i >= 0 {
		return list, i
	}
	return append([]string{v}, list...), 0
}

func (s *styleStep) Enter() tea.Cmd {
	p := s.env.Profile
	toggle := func(label string, list []string, v string) kit.Field {
		c, i := choicesWith(list, v)
		return kit.NewToggleField(label, c, i)
	}
	emoji := 0
	if p.Writing.Emoji {
		emoji = 1
	}
	lengthDefault := p.Writing.Length
	if lengthDefault == "" {
		lengthDefault = principal.Lengths[1]
	}
	s.form = kit.NewForm("you",
		toggle("you are a", principal.Roles, p.Role),
		toggle("tone", principal.Tones, p.Writing.Tone),
		toggle("length", principal.Lengths, lengthDefault),
		kit.NewToggleField("emoji", yesNo, emoji),
		kit.NewTextField("language", orDefault(p.Writing.Language, principal.Language)),
		toggle("replies", principal.Orders, p.Reading.Order),
		toggle("as", principal.Formats, p.Reading.Format),
		toggle("detail", principal.Details, p.Reading.Detail),
	)
	return nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func (s *styleStep) HandleKey(key string) Action {
	switch s.form.HandleKey(key) {
	case kit.FormCancel:
		return Skip
	case kit.FormSubmit:
		return Next
	}
	return Stay
}

func (s *styleStep) CapturesText() bool { return true }

func (s *styleStep) profile() settings.Profile {
	v := s.form.Values()
	return settings.Profile{
		Role: v[0],
		Writing: settings.ProfileWriting{Tone: v[1], Length: v[2], Emoji: v[3] == "yes",
			Language: strings.TrimSpace(v[4])},
		Reading: settings.ProfileReading{Order: v[5], Format: v[6], Detail: v[7]},
	}
}

func (s *styleStep) Apply() error {
	p := s.profile()
	if err := s.env.SaveProfile(p); err != nil {
		s.form.SetError(err.Error())
		return err
	}
	s.env.Profile = p
	s.env.Applied = append(s.env.Applied, "your profile saved: "+p.Role+", "+p.Writing.Tone+" writing, "+p.Reading.Format+" replies")
	return nil
}

func (s *styleStep) View(w, f int) []string {
	out := []string{theme.TextStyle().Render("How do you work?")}
	for _, l := range kit.WrapWords("Your employees deliver work in your name. Tell them how you write, and how you like to read their replies. Change it any time in Settings → CONFIG.", w) {
		out = append(out, dim(l))
	}
	out = append(out, "")
	l := s.form.ViewWidth(w)
	out = append(out, l[:len(l)-1]...)
	out = append(out, "", dim("Writing: commits, PR descriptions and comments written for you."),
		dim("Replies: how employees answer you in chat."), "")
	who := ""
	if s.env.WhoName != "" {
		who = s.env.WhoName
	}
	sample := s.profile().Principal(who, "")
	line := strings.SplitN(sample.Voice(), "\n", 4)[2]
	if who == "" {
		line = "You read" + strings.TrimPrefix(line, "the user reads")
	}
	for _, l := range kit.WrapWords(line, w) {
		out = append(out, theme.AccentText().Render(l))
	}
	return out
}
