package settings

import (
	"context"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// ---- git identity row (F-083) ----

// loadIdentity reads the git identity once and caches it.
func (m *Model) loadIdentity() (gitcore.Identity, bool) {
	if m.ident == nil {
		var id gitcore.Identity
		ok := false
		if m.d.Identity != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			got, err := m.d.Identity(ctx)
			cancel()
			id, ok = got, err == nil
		}
		m.ident, m.identOK = &id, ok
	}
	return *m.ident, m.identOK
}

func (m *Model) identityValue() string {
	if m.d.Identity == nil {
		return theme.TextMuted().Render("unavailable (git is not installed yet)")
	}
	id, ok := m.loadIdentity()
	if !ok {
		return theme.WarningText().Render("not set — enter to set it")
	}
	return id.Name + " <" + id.Email + ">"
}

func (m *Model) openIdentityForm() {
	if m.d.Identity == nil || m.d.SetIdentity == nil {
		m.flash = "git identity can't be changed here: git is not installed yet"
		return
	}
	id, ok := m.loadIdentity()
	title := "git identity"
	if !ok && m.d.SuggestIdentity != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		best := gitcore.BestSuggestion(m.d.SuggestIdentity(ctx))
		cancel()
		id = best.Identity
		if best.Source != "" {
			title = "git identity — suggested " + best.Source
		}
	}
	m.openFormDialog(title, dlgIdentity, "",
		kit.NewTextField("name", id.Name), kit.NewTextField("email", id.Email))
}

// submitIdentity validates and shows the exact commands before running.
func (m *Model) submitIdentity() {
	v := m.dform.Values()
	want := gitcore.Identity{Name: strings.TrimSpace(v[0]), Email: strings.TrimSpace(v[1])}
	if err := gitcore.ValidateIdentity(want); err != nil {
		m.dform.SetError(err.Error())
		return
	}
	m.wantID = want
	lines := []string{"DHI will run:", ""}
	for _, c := range gitcore.IdentityCommands(want) {
		lines = append(lines, "  "+theme.AccentText().Render(c))
	}
	lines = append(lines, "", theme.Hint().Render("enter run · esc cancel"))
	m.dform = nil
	m.dlg = &kit.Modal{Title: "set git identity", Lines: lines}
	m.dkind = dlgIdentityConfirm
}

func (m *Model) applyIdentity() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.d.SetIdentity(ctx, m.wantID); err != nil {
		m.dlg.Lines = append(m.dlg.Lines[:2], theme.DangerText().Render(err.Error()))
		return
	}
	m.ident, m.identOK = &m.wantID, true
	m.closeDialog()
	m.flash = "git identity set — your commits and your employees' carry " + m.wantID.Name
}
