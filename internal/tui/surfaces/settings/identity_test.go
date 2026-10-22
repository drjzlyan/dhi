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
	if v := ansi.Strip(m.View()); !strings.Contains(v, "not set — enter to set it") {
		t.Fatalf("unset row:\n%s", v)
	}
	feed(m, "enter")
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
	feed(m, "enter", "enter")
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
	feed(m, "enter", "tab", "ctrl+e")
	for i := 0; i < len("ada@x.io"); i++ {
		feed(m, "backspace")
	}
	feed(m, "n", "o", "p", "e", "enter")
	if m.dkind != dlgIdentity || !strings.Contains(m.dform.Err, "email") {
		t.Fatalf("bad email accepted: %+v", m.dform)
	}
}
