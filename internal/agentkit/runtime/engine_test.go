package runtime

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
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
