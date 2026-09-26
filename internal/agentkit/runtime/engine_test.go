package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/agentkit/scopes"
)

func TestEngineResolution(t *testing.T) {
	t.Run("manifest engine wins over the default", func(t *testing.T) {
		r := &Runtime{cfg: Config{DefaultEngine: "cli:claude"}}
		n, err := r.engineName(&manifest.Agent{Runtime: "codex", Engine: "cli:codex"})
		if err != nil || n != "codex" {
			t.Fatalf("engineName = %q err = %v, want codex", n, err)
		}
	})

	t.Run("agent with no engine inherits the default", func(t *testing.T) {
		r := &Runtime{cfg: Config{DefaultEngine: "cli:claude"}}
		n, err := r.engineName(&manifest.Agent{})
		if err != nil || n != "claude" {
			t.Fatalf("engineName = %q err = %v, want claude", n, err)
		}
	})

	t.Run("no engine anywhere refuses by name", func(t *testing.T) {
		r := &Runtime{cfg: Config{}}
		_, err := r.engineName(&manifest.Agent{})
		if err == nil || !strings.Contains(err.Error(), "no engine") {
			t.Fatalf("err = %v, want a named no-engine refusal", err)
		}
	})

	t.Run("unregistered default refuses", func(t *testing.T) {
		r := &Runtime{cfg: Config{DefaultEngine: "cli:nope"}}
		if _, err := r.engineName(&manifest.Agent{}); err == nil {
			t.Fatal("unregistered default engine must refuse")
		}
	})
}

func TestAgentScopesLayering(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "schema = 1\n[teams.backend]\nmembers = [\"scout\"]\n[teams.backend.scopes]\nwrite = \"deny\"\n"
	if err := os.WriteFile(filepath.Join(root, ".dhi", "org.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := org.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{cfg: Config{Org: o}}

	if got := r.agentScopes(&manifest.Agent{ID: "scout"}); got.EffectFor(scopes.Write) != scopes.Deny {
		t.Fatalf("team scope not applied: write = %q", got.EffectFor(scopes.Write))
	}
	// Manifest override wins over the team layer.
	over := &manifest.Agent{ID: "scout", Scopes: scopes.Set{scopes.Write: scopes.Auto}}
	if got := r.agentScopes(over); got.EffectFor(scopes.Write) != scopes.Auto {
		t.Fatalf("manifest override lost: write = %q", got.EffectFor(scopes.Write))
	}
	// Unlisted scope keeps the default.
	if got := r.agentScopes(&manifest.Agent{ID: "scout"}); got.EffectFor(scopes.Read) != scopes.Auto {
		t.Fatalf("read default changed: %q", got.EffectFor(scopes.Read))
	}
}
