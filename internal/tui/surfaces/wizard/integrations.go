package wizard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// IntegrationRow is one vendor's official plugin as setup offers it
// (ADR-0043): Atlassian, Slack, Zoom, Linear, Notion, GitHub.
type IntegrationRow struct {
	Name        string // display name ("Atlassian")
	Plugin      string // the plugin's name in the marketplace ("atlassian")
	Description string
	Installed   bool
	// SignIn says how it authorizes: "sign in in your browser on first
	// use", or what it needs instead (Zoom's token).
	SignIn string
}

// Who gets a newly added integration.
const (
	whoRecommended = "the bots it fits"
	whoEveryone    = "every bot"
	whoNobody      = "nobody yet"
)

type intPhase uint8

const (
	intList intPhase = iota
	intWho
	intInstalling
)

type intInstalledMsg struct {
	name string
	note string
	err  error
}

// integrationsStep adds the vendors' official plugins (ADR-0043): their
// remote MCP servers and skills, installed the standard way; agents sign
// in through the vendor's own login on first use — no app, no API key.
type integrationsStep struct {
	base
	env     *Env
	rows    []IntegrationRow
	cur     int
	phase   intPhase
	who     *kit.Form
	msg     string
	err     string
	pending tea.Cmd
}

func (*integrationsStep) ID() string    { return "integrations" }
func (*integrationsStep) Title() string { return "integrations" }

func (s *integrationsStep) Applies() bool {
	return s.env.Root != "" && s.env.IntegrationRows != nil && s.env.SetupIntegration != nil
}

func (s *integrationsStep) Enter() tea.Cmd {
	s.phase, s.msg, s.err = intList, "", ""
	s.refresh()
	return nil
}

func (s *integrationsStep) refresh() {
	s.rows = s.env.IntegrationRows()
	if s.cur >= len(s.rows) {
		s.cur = max(len(s.rows)-1, 0)
	}
}

func (s *integrationsStep) selected() (IntegrationRow, bool) {
	if s.cur < 0 || s.cur >= len(s.rows) {
		return IntegrationRow{}, false
	}
	return s.rows[s.cur], true
}

// Busy blocks navigation while a plugin installs.
func (s *integrationsStep) Busy() bool { return s.phase == intInstalling }

func (s *integrationsStep) TakeCmd() tea.Cmd {
	c := s.pending
	s.pending = nil
	return c
}

func (s *integrationsStep) HandleKey(key string) Action {
	row, ok := s.selected()
	switch s.phase {
	case intWho:
		switch s.who.HandleKey(key) {
		case kit.FormCancel:
			s.phase = intList
		case kit.FormSubmit:
			s.start(row, s.who.Values()[0])
		}
		return Stay
	case intInstalling:
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
	case "s", "space", " ":
		if ok && !row.Installed {
			s.msg, s.err = "", ""
			s.who = kit.NewForm("who", kit.NewToggleField("give it to", []string{whoRecommended, whoEveryone, whoNobody}, 0))
			s.phase = intWho
		}
	case "enter":
		return Next
	case "esc":
		return Skip
	}
	return Stay
}

func (s *integrationsStep) start(row IntegrationRow, who string) {
	s.phase = intInstalling
	setup := s.env.SetupIntegration
	s.pending = func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		note, err := setup(ctx, row.Plugin, who)
		return intInstalledMsg{name: row.Name, note: note, err: err}
	}
}

func (s *integrationsStep) Update(msg tea.Msg) tea.Cmd {
	m, ok := msg.(intInstalledMsg)
	if !ok {
		return nil
	}
	s.phase = intList
	if m.err != nil {
		s.err = fmt.Sprintf("could not add %s: %v", m.name, m.err)
		return nil
	}
	s.env.Changed = true // servers are read at launch
	s.env.Applied = append(s.env.Applied, "added "+m.name+" ("+m.note+")")
	s.msg = m.name + " added — " + m.note
	s.refresh()
	return nil
}

func (s *integrationsStep) status(r IntegrationRow) string {
	if r.Installed {
		return kit.Badge(theme.BadgeOK, "added")
	}
	return kit.Badge(theme.BadgeNeutral, "not added")
}

// Keys names the step's own keys for the status row, by phase.
func (s *integrationsStep) Keys() []string {
	switch s.phase {
	case intWho:
		return []string{"←/→ choose", "enter add", "esc back"}
	case intInstalling:
		return nil
	}
	keys := []string{"↑/↓ select"}
	if r, ok := s.selected(); ok && !r.Installed {
		keys = append(keys, "s add")
	}
	return keys
}

func (s *integrationsStep) View(w, f int) []string {
	out := []string{theme.TextStyle().Render("Connect the tools your bots work in")}
	for _, l := range kit.WrapWords("The vendors' own plugins: sign in with your account when a bot first uses one. Optional — more in Settings → INTEGRATIONS.", w) {
		out = append(out, dim(l))
	}
	out = append(out, "")
	for i, r := range s.rows {
		mark, name := "  ", theme.TextStyle().Render(fmt.Sprintf("%-14s", r.Name))
		if i == s.cur {
			mark = theme.AccentText().Render(theme.GlyphCursor) + " "
			name = theme.AccentText().Render(fmt.Sprintf("%-14s", r.Name))
		}
		out = append(out, kit.ClipEllipsis(mark+name+s.status(r), w))
	}
	if row, ok := s.selected(); ok {
		out = append(out, "")
		for _, l := range kit.WrapWords(row.Description, w) {
			out = append(out, dim(l))
		}
		if row.SignIn != "" {
			out = append(out, ok2(row.SignIn))
		}
	}
	switch s.phase {
	case intWho:
		out = append(out, "")
		out = append(out, formLines(s.who)...)
	case intInstalling:
		out = append(out, "", dim("adding "+s.rows[s.cur].Name+"…"))
	}
	if s.msg != "" {
		out = append(out, "", ok(s.msg))
	}
	if s.err != "" {
		for _, l := range kit.WrapWords(s.err, w) {
			out = append(out, bad(l))
		}
	}
	return out
}

func ok2(s string) string { return theme.Hint().Render(strings.TrimSpace(s)) }
