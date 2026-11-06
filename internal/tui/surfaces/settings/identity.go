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

// identEvent carries an identity read or a suggestion back to the loop:
// both run git (and the suggestion GitHub), so never on the UI thread.
type identEvent struct {
	id      gitcore.Identity
	ok      bool
	suggest bool // a suggestion for the open identity form, not the row
	source  string
}

// loadIdentity returns the cached git identity; the first call starts
// reading it in the background (loaded=false until the event lands).
func (m *Model) loadIdentity() (id gitcore.Identity, ok, loaded bool) {
	if m.ident != nil {
		return *m.ident, m.identOK, true
	}
	if m.d.Identity != nil && !m.identLoading {
		m.identLoading = true
		resolve, ch := m.d.Identity, m.events
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			got, err := resolve(ctx)
			cancel()
			ch <- settingsEvent{ident: &identEvent{id: got, ok: err == nil}}
		}()
	}
	return gitcore.Identity{}, false, false
}

func (m *Model) onIdentEvent(ev identEvent) {
	if !ev.suggest {
		m.identLoading = false
		m.ident, m.identOK = &ev.id, ev.ok
		return
	}
	if m.dkind != dlgIdentity || m.dform == nil || m.dlg == nil {
		return
	}
	title := "git identity"
	if ev.source != "" {
		title += " — suggested " + ev.source
	}
	m.dform.Title, m.dlg.Title = title, title
	// Fill the open form only while it is still empty: never over typing.
	if v := m.dform.Values(); strings.TrimSpace(v[0]+v[1]) == "" {
		m.dform.Fields[0] = kit.NewTextField("name", ev.id.Name)
		m.dform.Fields[1] = kit.NewTextField("email", ev.id.Email)
	}
}

func (m *Model) identityValue() string {
	if m.d.Identity == nil {
		return theme.TextMuted().Render("unavailable (git is not installed yet)")
	}
	id, ok, loaded := m.loadIdentity()
	switch {
	case !loaded:
		return theme.TextMuted().Render("checking…")
	case !ok:
		return theme.WarningText().Render("not set — enter to set it")
	}
	return id.Name + " <" + id.Email + ">"
}

func (m *Model) openIdentityForm() {
	if m.d.Identity == nil || m.d.SetIdentity == nil {
		m.flash = "git identity can't be changed here: git is not installed yet"
		return
	}
	id, ok, loaded := m.loadIdentity()
	if !loaded {
		m.flash = "still reading your git identity — try again in a moment"
		return
	}
	title := "git identity"
	if !ok && m.d.SuggestIdentity != nil {
		// Suggestions look at the repos, GitHub and the account: the form
		// opens now and fills in when they arrive.
		title = "git identity — looking for a suggestion…"
		suggest, ch := m.d.SuggestIdentity, m.events
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			best := gitcore.BestSuggestion(suggest(ctx))
			cancel()
			ch <- settingsEvent{ident: &identEvent{id: best.Identity, suggest: true, source: best.Source}}
		}()
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
	lines := []string{"For all your repos, DHI writes ~/.gitconfig:", ""}
	for _, c := range gitcore.IdentityCommands(want) {
		lines = append(lines, "  "+theme.AccentText().Render(c))
	}
	hint := "enter all my repos · esc cancel"
	if m.d.SetIdentityLocal != nil {
		lines = append(lines, "", "Or keep it to this workspace (each repo's .git/config, repos added later too):")
		for _, c := range gitcore.LocalIdentityCommands(want) {
			lines = append(lines, "  "+theme.AccentText().Render(c))
		}
		hint = "enter all my repos · w this workspace only · esc cancel"
	}
	lines = append(lines, "", theme.Hint().Render(hint))
	m.dform = nil
	m.dlg = &kit.Modal{Title: "set git identity", Lines: lines}
	m.dkind = dlgIdentityConfirm
}

func (m *Model) applyIdentity(local bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	set, where := m.d.SetIdentity, "saved to ~/.gitconfig"
	if local {
		set, where = m.d.SetIdentityLocal, "saved to this workspace's repos only"
	}
	if err := set(ctx, m.wantID); err != nil {
		m.dlg.Lines = append(m.dlg.Lines, theme.DangerText().Render(err.Error()))
		return
	}
	m.ident, m.identOK = &m.wantID, true
	m.closeDialog()
	m.flash = "git identity " + where + " — your commits and your employees' carry " + m.wantID.Name
}
