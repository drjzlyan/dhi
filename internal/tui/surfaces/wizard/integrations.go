package wizard

import (
	"fmt"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/catalog"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// IntegrationRow is one catalog entry with its state on this machine.
type IntegrationRow struct {
	Entry catalog.Entry
	State catalog.State
}

// Who gets a newly connected integration.
const (
	whoEveryone = "every employee"
	whoLead     = "the team lead only"
	whoNobody   = "nobody yet"
)

type intPhase uint8

const (
	intList intPhase = iota
	intInfo
	intFields
	intWho
	intConfirm
)

// integrationsStep connects Jira, Slack and the rest (F-047, ADR-0028):
// shows who runs each server and how far to trust it, collects the
// credentials with masked input, and says exactly where they will be stored
// before anything is written.
type integrationsStep struct {
	base
	env    *Env
	rows   []IntegrationRow
	cur    int
	phase  intPhase
	form   *kit.Form
	who    *kit.Form
	inputs map[string]string
	msg    string
	err    string
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

// CapturesText: the credential fields take typed text.
func (s *integrationsStep) CapturesText() bool { return s.phase == intFields }

func (s *integrationsStep) HandleKey(key string) Action {
	row, ok := s.selected()
	switch s.phase {
	case intInfo:
		switch key {
		case "enter":
			s.openFields(row)
		case "esc":
			s.phase = intList
		}
		return Stay
	case intFields:
		switch s.form.HandleKey(key) {
		case kit.FormCancel:
			s.phase = intInfo
		case kit.FormSubmit:
			s.submitFields(row)
		}
		return Stay
	case intWho:
		switch s.who.HandleKey(key) {
		case kit.FormCancel:
			s.phase = intFields
		case kit.FormSubmit:
			s.phase = intConfirm
		}
		return Stay
	case intConfirm:
		switch key {
		case "enter", "y":
			s.apply(row)
		case "esc", "n":
			s.phase = intWho
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
	case "s", " ":
		if ok && row.Entry.Unavailable == "" {
			s.msg, s.err = "", ""
			s.phase = intInfo
		}
	case "enter":
		return Next
	case "esc":
		return Skip
	}
	return Stay
}

func (s *integrationsStep) openFields(row IntegrationRow) {
	fields := make([]kit.Field, 0, len(row.Entry.Inputs))
	for _, in := range row.Entry.Inputs {
		label := fmt.Sprintf("%-10s", in.Label)
		if in.Secret {
			fields = append(fields, kit.NewSecretField(label))
		} else {
			fields = append(fields, kit.NewTextField(label, ""))
		}
	}
	s.form = kit.NewForm(row.Entry.Name, fields...)
	s.phase = intFields
}

func (s *integrationsStep) submitFields(row IntegrationRow) {
	vals := s.form.Values()
	in := map[string]string{}
	for i, f := range row.Entry.Inputs {
		in[f.Key] = strings.TrimSpace(vals[i])
	}
	// Validate now (pure) so a typo is caught before any "where it is
	// stored" screen; Install re-validates before writing.
	if row.Entry.Derive != nil {
		if _, err := row.Entry.Derive(in); err != nil {
			s.form.SetError(err.Error())
			return
		}
	}
	for _, f := range row.Entry.Inputs {
		if in[f.Key] == "" {
			s.form.SetError(f.Label + " is required")
			return
		}
	}
	s.inputs = in
	choices := []string{whoEveryone, whoLead, whoNobody}
	s.who = kit.NewForm("who", kit.NewToggleField("give it to", choices, 0))
	s.phase = intWho
}

func (s *integrationsStep) apply(row IntegrationRow) {
	who := s.who.Values()[0]
	enabled, err := s.env.SetupIntegration(row.Entry.Slug, s.inputs, who)
	s.inputs = nil // the secrets leave memory with the form
	if err != nil {
		s.err = fmt.Sprintf("could not connect %s: %v", row.Entry.Name, err)
		s.phase = intList
		s.refresh()
		return
	}
	s.env.Changed = true // cards are read at launch
	line := "connected " + row.Entry.Name
	if len(enabled) > 0 {
		line += " (" + strings.Join(enabled, ", ") + ")"
	}
	s.env.Applied = append(s.env.Applied, line)
	s.msg = row.Entry.Name + " connected"
	if len(enabled) == 0 {
		s.msg += " — no employee has access yet"
	}
	s.phase = intList
	s.refresh()
}

func (s *integrationsStep) status(r IntegrationRow) string {
	switch {
	case r.Entry.Unavailable != "":
		return dim("not available yet")
	case r.State.Ready():
		return ok("connected")
	case r.State.Installed:
		return warn("needs credentials: " + strings.Join(r.State.Missing, ", "))
	default:
		return dim("not connected · s sets it up")
	}
}

func trustLabel(t catalog.Trust) string {
	switch t {
	case catalog.TrustVendorHosted:
		return ok("run by the vendor")
	case catalog.TrustVendor:
		return ok("published by the vendor, runs locally")
	default:
		return warn("community project, runs locally with your token")
	}
}

func (s *integrationsStep) View(w, f int) []string {
	out := []string{
		theme.TextStyle().Render("Connect the tools your employees work in"),
		dim("Optional — you can do this later from the wizard (ctrl+p → Run setup wizard)."), "",
	}
	for i, r := range s.rows {
		mark, name := "  ", theme.TextStyle().Render(fmt.Sprintf("%-20s", r.Entry.Name))
		if i == s.cur {
			mark = theme.GlyphCursor + " "
			name = theme.TabActive().Render(fmt.Sprintf("%-20s", r.Entry.Name))
		}
		out = append(out, mark+name+s.status(r))
	}
	row, hasRow := s.selected()
	if !hasRow {
		return out
	}
	e := row.Entry
	switch s.phase {
	case intInfo:
		out = append(out, "", theme.AccentText().Render(e.Name)+dim(" — "+e.Description), "", trustLabel(e.Trust))
		for _, l := range kit.WrapWords(e.TrustNote, w) {
			out = append(out, dim(l))
		}
		out = append(out, "", theme.TextStyle().Render("Before you continue:"))
		for i, st := range e.Steps {
			for j, l := range kit.WrapWords(st, w-5) {
				prefix := "   "
				if j == 0 {
					prefix = fmt.Sprintf("%d. ", i+1)
				}
				out = append(out, "  "+dim(prefix+l))
			}
		}
		out = append(out, "", dim("create it here: ")+theme.AccentText().Render(e.TokenURL),
			dim("enter continue · esc back"))
	case intFields:
		out = append(out, "", theme.AccentText().Render(e.Name)+dim(" — enter the details (hidden as you type)"), "")
		out = append(out, formLines(s.form)...)
		for _, in := range e.Inputs {
			if in.Help != "" {
				out = append(out, dim(fmt.Sprintf("  %s: %s", in.Label, in.Help)))
			}
		}
		out = append(out, dim("tab next field · enter continue · esc back"))
	case intWho:
		out = append(out, "", theme.AccentText().Render(e.Name), "")
		out = append(out, formLines(s.who)...)
		out = append(out, dim("←/→ choose · enter continue · esc back"))
	case intConfirm:
		path := "your credentials file"
		if s.env.CredentialsPath != "" {
			path = s.env.CredentialsPath
		}
		out = append(out, "", theme.TextStyle().Render("DHI will:"), "",
			"  "+fmt.Sprintf("store %d credential(s) in %s", len(e.Credentials()), theme.AccentText().Render(path)),
			"  "+dim("(owner-only file, readable plain text — like ~/.aws/credentials)"),
			"  "+fmt.Sprintf("add %s to this workspace (no secrets in it, safe to commit)", theme.AccentText().Render(".dhi/mcp/"+e.Slug+".toml")),
			"  "+fmt.Sprintf("give %s access, each call still asks you first", s.who.Values()[0]),
			"", dim("enter / y to connect · esc back"))
	default:
		if e.Unavailable != "" {
			out = append(out, "")
			for _, l := range kit.WrapWords(e.Unavailable, w) {
				out = append(out, warn(l))
			}
		} else {
			out = append(out, "", dim(e.Description), trustLabel(e.Trust))
		}
		out = append(out, "", dim("↑/↓ select · s set up · enter continue"))
	}
	if s.msg != "" {
		out = append(out, "", ok(s.msg))
	}
	if s.err != "" {
		out = append(out, "", bad(s.err))
	}
	return out
}
