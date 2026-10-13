package workspace

import (
	"strings"

	"github.com/drjzlyan/dhi/internal/agentkit/tools"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

var secretScopes = []string{"this workspace", "every workspace"}

// decideAsk answers an agent's request from the chat pane or the inbox
// (F-087/F-089). A secret whose value is not stored yet opens a masked
// form first; the answer is given once the value is saved.
func (m *Model) decideAsk(ap *tools.Approval, choice string) string {
	if m.approvals == nil {
		return ""
	}
	if ap.IsSecret() && ap.Missing && choice != "no" {
		if m.secrets == nil {
			return "can't store secrets here (no user config dir)"
		}
		m.secretAsk, m.secretChoice = ap, choice
		at := 0
		if choice == "everywhere" {
			at = 1
		}
		m.form = openForm(fSecret, ap.SecretName(),
			kit.NewSecretField(ap.SecretName()),
			toggleFieldAt("save for ", secretScopes, at))
		return "type " + ap.SecretName() + " (masked)"
	}
	m.approvals.Decide(ap, choice)
	return choice + ": " + ap.Agent + " " + ap.Describe()
}

// submitSecret saves the typed value where chosen, then answers the
// request. The value goes straight to the credential store: never the
// bus, the request, or the agent.
func (m *Model) submitSecret() {
	f := &m.form
	ap := m.secretAsk
	if ap == nil {
		m.closeForm()
		return
	}
	if _, ok := m.approvals.Get(ap.ID); !ok {
		f.err = "the request is gone (the agent stopped waiting)"
		return
	}
	value := strings.TrimSpace(f.values()[0])
	if value == "" {
		f.err = "type the value, or esc"
		return
	}
	scope := f.values()[1]
	root := m.ws.Root
	if scope == secretScopes[1] {
		root = ""
	}
	if err := m.secrets.SetIn(root, ap.SecretName(), value); err != nil {
		f.err = err.Error()
		return
	}
	m.approvals.Decide(ap, m.secretChoice)
	name := ap.SecretName()
	m.secretAsk, m.secretChoice = nil, ""
	m.closeForm()
	m.form.flash = "saved " + name + " for " + scope + "; " + ap.Agent + " can use it"
}
