package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/gitcore"
)

func TestIdentityRowSuggestsConfirmsAndSets(t *testing.T) {
	m, _ := newSurface(t)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "unavailable") {
		t.Fatalf("no git: row should say unavailable:\n%s", v)
	}
	var set []gitcore.Identity
	m.d.Identity = func(context.Context) (gitcore.Identity, error) { return gitcore.Identity{}, gitcore.ErrIdentityUnset }
	m.d.SetIdentity = func(_ context.Context, id gitcore.Identity) error { set = append(set, id); return nil }
	m.d.SuggestIdentity = func(context.Context) []gitcore.Suggestion {
		return []gitcore.Suggestion{{Identity: gitcore.Identity{Name: "Ada", Email: "ada@x.io"}, Source: "your GitHub account (ada)"}}
	}
	m.ident = nil
	m.cursor = rowIdentity
	m.View() // the first render starts reading the identity
	drainDialogEvent(t, m)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "not set — enter to set it") {
		t.Fatalf("unset row:\n%s", v)
	}
	feed(m, "enter")
	if !strings.Contains(m.dlg.Title, "looking for a suggestion") {
		t.Fatalf("the form should open at once while suggestions load: %q", m.dlg.Title)
	}
	drainDialogEvent(t, m)
	if m.dkind != dlgIdentity || m.dform.Values()[0] != "Ada" || !strings.Contains(m.dlg.Title, "your GitHub account") {
		t.Fatalf("form %+v title %q", m.dform, m.dlg.Title)
	}
	feed(m, "enter")
	if m.dkind != dlgIdentityConfirm || !strings.Contains(ansi.Strip(m.View()), "git config --global user.email 'ada@x.io'") {
		t.Fatalf("confirm:\n%s", ansi.Strip(m.View()))
	}
	feed(m, "enter")
	if len(set) != 1 || set[0].Email != "ada@x.io" || m.dlg != nil {
		t.Fatalf("set %+v dlg %v", set, m.dlg != nil)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Ada <ada@x.io>") {
		t.Fatalf("row not refreshed:\n%s", v)
	}
}

func TestIdentityCanBeKeptToTheWorkspace(t *testing.T) {
	m, _ := newSurface(t)
	var global, local []gitcore.Identity
	m.d.Identity = func(context.Context) (gitcore.Identity, error) { return gitcore.Identity{}, gitcore.ErrIdentityUnset }
	m.d.SetIdentity = func(_ context.Context, id gitcore.Identity) error { global = append(global, id); return nil }
	m.d.SetIdentityLocal = func(_ context.Context, id gitcore.Identity) error { local = append(local, id); return nil }
	m.d.SuggestIdentity = func(context.Context) []gitcore.Suggestion {
		return []gitcore.Suggestion{{Identity: gitcore.Identity{Name: "Ada", Email: "ada@x.io"}, Source: "x"}}
	}
	m.ident = nil
	m.cursor = rowIdentity
	m.View()
	drainDialogEvent(t, m)
	feed(m, "enter")
	drainDialogEvent(t, m) // the suggestion
	feed(m, "enter")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "w this workspace only") {
		t.Fatalf("confirm:\n%s", v)
	}
	feed(m, "w")
	if len(local) != 1 || len(global) != 0 || !strings.Contains(m.flash, "this workspace's repos only") {
		t.Fatalf("local=%v global=%v flash=%q", local, global, m.flash)
	}
}

func TestIdentityFormRefusesBadEmail(t *testing.T) {
	m, _ := newSurface(t)
	m.d.Identity = func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{Name: "Ada", Email: "ada@x.io"}, nil
	}
	m.d.SetIdentity = func(context.Context, gitcore.Identity) error { return nil }
	m.cursor = rowIdentity
	m.View()
	drainDialogEvent(t, m)
	feed(m, "enter", "tab", "ctrl+e")
	for i := 0; i < len("ada@x.io"); i++ {
		feed(m, "backspace")
	}
	feed(m, "n", "o", "p", "e", "enter")
	if m.dkind != dlgIdentity || !strings.Contains(m.dform.Err, "email") {
		t.Fatalf("bad email accepted: %+v", m.dform)
	}
}

// Reading the identity runs git: the row renders "checking…" while it
// runs and never waits for it; enter on the row waits too, with a note.
func TestIdentityRowNeverBlocksTheRender(t *testing.T) {
	m, _ := newSurface(t)
	release := make(chan struct{})
	m.d.Identity = func(context.Context) (gitcore.Identity, error) {
		<-release
		return gitcore.Identity{Name: "Ada", Email: "ada@x.io"}, nil
	}
	m.d.SetIdentity = func(context.Context, gitcore.Identity) error { return nil }
	m.ident = nil
	m.cursor = rowIdentity
	if v := ansi.Strip(m.View()); !strings.Contains(v, "checking…") {
		t.Fatalf("row while git runs:\n%s", v)
	}
	m.View() // a second render must not start a second read
	feed(m, "enter")
	if m.dlg != nil || !strings.Contains(m.flash, "still reading") {
		t.Fatalf("enter before the identity landed: dlg=%v flash=%q", m.dlg != nil, m.flash)
	}
	close(release)
	drainDialogEvent(t, m)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Ada <ada@x.io>") {
		t.Fatalf("row after the read:\n%s", v)
	}
	select {
	case ev := <-m.events:
		t.Fatalf("a second identity read ran: %+v", ev)
	default:
	}
}
