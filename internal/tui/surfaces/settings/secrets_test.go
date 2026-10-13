package settings

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/credstore"
)

type memSecretStore struct{ m map[string]string } // root|name → value

func (s *memSecretStore) Secrets(root string) ([]credstore.Secret, error) {
	var out []credstore.Secret
	for k := range s.m {
		r, n, _ := strings.Cut(k, "|")
		if r == root || r == "" {
			out = append(out, credstore.Secret{Name: n, Workspace: r})
		}
	}
	return out, nil
}
func (s *memSecretStore) SetIn(root, name, value string) error {
	s.m[root+"|"+name] = value
	return nil
}
func (s *memSecretStore) DeleteIn(root, name string) error { delete(s.m, root+"|"+name); return nil }

// F-089: SECRETS adds a secret through a masked field and lists names
// and scopes only — the value never renders.
func TestSecretsSectionAddsMaskedAndListsNamesOnly(t *testing.T) {
	m, _, _, _ := agentSurface(t)
	store := &memSecretStore{m: map[string]string{}}
	m.d.Secrets = store
	for m.sec != secSecrets {
		feed(m, "]")
	}
	m.HandleKey("a")
	for _, r := range "API_TOKEN" {
		m.HandleKey(string(r))
	}
	m.HandleKey("tab")
	for _, r := range "tok_live_999" {
		m.HandleKey(string(r))
	}
	if strings.Contains(ansi.Strip(m.View()), "tok_live") {
		t.Fatal("value shown while typing")
	}
	m.HandleKey("tab")
	m.HandleKey("enter")
	if len(store.m) != 1 {
		t.Fatalf("store = %v", store.m)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "API_TOKEN") || strings.Contains(view, "tok_live") {
		t.Fatalf("list:\n%s", view)
	}
}
