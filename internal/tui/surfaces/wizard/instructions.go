package wizard

import (
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

var instructionScopes = []string{"every workspace", "this workspace"}

// suggestedRules are house rules most teams want, offered unticked so a
// rule is never set without the user choosing it (F-098).
var suggestedRules = []string{
	"Write or update tests with every change",
	"Keep commits small and focused",
	"Ask before adding a dependency",
	"Explain the why in commit messages",
	"Never commit secrets or credentials",
}

// rule is one row of the list: its text, whether it is on, and whether
// the user typed it (custom rows can be removed).
type rule struct {
	text   string
	on     bool
	custom bool
}

// instructionsStep asks what every employee should always do (F-090):
// a list of rules — suggestions to tick and your own to add (F-098) —
// saved for every workspace by default, so a new workspace needs no
// setup. Enter with nothing ticked skips.
type instructionsStep struct {
	base
	env    *Env
	rules  []rule
	cur    int
	scope  int // index into the offered scopes
	adding bool
	input  kit.Input
	err    string
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

func (s *instructionsStep) scopes() []string {
	if s.env.Root == "" {
		return instructionScopes[:1] // no workspace yet: only yours
	}
	return instructionScopes
}

func (s *instructionsStep) Enter() tea.Cmd {
	mine, here := s.env.Instructions()
	s.scope, s.cur, s.adding, s.err = 0, 0, false, ""
	existing := mine
	if len(mine) == 0 && len(here) > 0 && s.env.Root != "" {
		s.scope, existing = 1, here
	}
	s.rules = s.rules[:0]
	seen := map[string]bool{}
	for _, l := range existing {
		s.rules = append(s.rules, rule{text: l, on: true, custom: true})
		seen[strings.ToLower(l)] = true
	}
	for _, l := range suggestedRules {
		if !seen[strings.ToLower(l)] {
			s.rules = append(s.rules, rule{text: l})
		}
	}
	return nil
}

func (s *instructionsStep) HandleKey(key string) Action {
	if s.adding {
		switch key {
		case "esc":
			s.adding = false
		case "enter":
			if t := strings.TrimSpace(s.input.Value); t != "" {
				s.rules = append(s.rules, rule{text: t, on: true, custom: true})
				s.cur = len(s.rules) - 1
			}
			s.adding = false
		default:
			s.input.HandleKey(key)
		}
		return Stay
	}
	switch key {
	case "esc":
		return Skip
	case "enter":
		return Next
	case "j", "down":
		s.cur = min(s.cur+1, len(s.rules)-1)
	case "k", "up":
		s.cur = max(s.cur-1, 0)
	case " ", "space", "x":
		if s.cur < len(s.rules) {
			s.rules[s.cur].on = !s.rules[s.cur].on
		}
	case "a":
		s.adding, s.input = true, kit.NewInput("", "e.g. Use British English")
		s.input.Focused = true
	case "d":
		if s.cur < len(s.rules) && s.rules[s.cur].custom {
			s.rules = append(s.rules[:s.cur], s.rules[s.cur+1:]...)
			s.cur = min(s.cur, max(len(s.rules)-1, 0))
		}
	case "s", "tab":
		s.scope = (s.scope + 1) % len(s.scopes())
	}
	return Stay
}

// CapturesText: only while a rule is being typed.
func (s *instructionsStep) CapturesText() bool { return s.adding }

// Keys names the step's own keys for the status row.
func (s *instructionsStep) Keys() []string {
	if s.adding {
		return []string{"enter add", "esc cancel"}
	}
	keys := []string{"space tick", "a add your own"}
	if s.cur < len(s.rules) && s.rules[s.cur].custom {
		keys = append(keys, "d delete")
	}
	if len(s.scopes()) > 1 {
		keys = append(keys, "s scope")
	}
	return keys
}

// lines are the ticked rules, in order.
func (s *instructionsStep) lines() []string {
	var out []string
	for _, r := range s.rules {
		if r.on {
			out = append(out, r.text)
		}
	}
	return out
}

func (s *instructionsStep) Apply() error {
	lines := s.lines()
	if len(lines) == 0 {
		return nil
	}
	everywhere := s.scopes()[s.scope] == instructionScopes[0]
	if err := s.env.SaveInstructions(lines, everywhere); err != nil {
		s.err = err.Error()
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
	out := []string{theme.TextStyle().Render("What should every agent always do?")}
	for _, l := range kit.WrapWords("Tick the house rules you want and add your own. Agents follow them on every task. "+
		"Change them any time in Settings → STANDARDS. Enter with nothing ticked skips.", w) {
		out = append(out, dim(l))
	}
	out = append(out, "")
	for i, r := range s.rules {
		row := kit.Checkbox(kit.ClipEllipsis(r.text, w-4), r.on, i == s.cur && !s.adding)
		if r.custom {
			row += theme.TextMuted().Render("  yours")
		}
		out = append(out, row)
	}
	if s.adding {
		out = append(out, "")
		out = append(out, s.input.View(w)...)
	}
	out = append(out, "")
	var scopes []string
	for i, sc := range s.scopes() {
		scopes = append(scopes, kit.Radio(sc, i == s.scope, false))
	}
	out = append(out, dim("for  ")+strings.Join(scopes, "   "))
	if s.err != "" {
		out = append(out, theme.DangerText().Render(theme.GlyphCross+" "+s.err))
	}
	return out
}
