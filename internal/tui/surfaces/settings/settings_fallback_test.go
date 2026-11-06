package settings

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
)

// withDefaultFallback installs the picker the app wires at start, over a
// fixed set of installed CLIs.
func withDefaultFallback(t *testing.T, installed ...string) {
	t.Helper()
	prev := org.DefaultFallback
	t.Cleanup(func() { org.DefaultFallback = prev })
	org.DefaultFallback = func(primary string) (string, string) { return clirun.PickFallback(primary, installed) }
}

func fillAgent(m *Model, id, name, model string) {
	for i, v := range []string{id, name, model} {
		for m.form.f.Cur() != i {
			feed(m, "tab")
		}
		for _, r := range v {
			m.HandleKey(string(r))
		}
	}
}

func rosterAgent(t *testing.T, m *Model, id string) *manifest.Agent {
	t.Helper()
	roster, err := org.LoadRoster(m.d.WS)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range roster {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("agent %s missing", id)
	return nil
}

// F-180: a new agent opens with the default fallback already chosen, and
// saving writes it into the manifest.
func TestNewAgentFormShowsAndSavesDefaultFallback(t *testing.T) {
	withDefaultFallback(t, "claude", "codex")
	m, _, _, _ := agentSurface(t)
	if !m.HandleKey("n") {
		t.Fatal("n refused")
	}
	if got := m.form.f.Fields[9].Toggle[m.form.f.ToggleIndex(9)]; got != "codex" {
		t.Fatalf("fallback preselect = %q, want codex (claude is the engine)", got)
	}
	fillAgent(m, "spec", "Spec", "m-1")
	feed(m, "enter")
	if m.form.f != nil && m.form.err != "" {
		t.Fatalf("submit err = %q", m.form.err)
	}
	a := rosterAgent(t, m, "spec")
	if a.FallbackEngine != "cli:codex" || a.FallbackRuntime != "codex" {
		t.Fatalf("fallback = %q/%q", a.FallbackEngine, a.FallbackRuntime)
	}
}

// The default is editable: "none" is a choice and is kept as one.
func TestAgentFallbackCanBeSetToNoneAndKeepsItsModel(t *testing.T) {
	withDefaultFallback(t, "claude", "codex")
	m, ws, o, _ := agentSurface(t)
	seed := &manifest.Agent{ID: "scout", Name: "Scout", Model: "m-1", Runtime: "claude",
		FallbackEngine: "cli:codex", FallbackRuntime: "codex", FallbackModel: "gpt-x"}
	if err := o.CreateAgent(ws, seed); err != nil {
		t.Fatal(err)
	}
	if !m.HandleKey("e") {
		t.Fatal("e refused")
	}
	if m.form.f.Fields[9].Toggle[m.form.f.ToggleIndex(9)] != "codex" || m.form.f.Fields[10].Value != "gpt-x" {
		t.Fatalf("edit prefill = %q / %q", m.form.f.Fields[9].Toggle[m.form.f.ToggleIndex(9)], m.form.f.Fields[10].Value)
	}
	feed(m, "enter")
	if a := rosterAgent(t, m, "scout"); a.FallbackRuntime != "codex" || a.FallbackModel != "gpt-x" {
		t.Fatalf("unchanged edit lost the fallback: %+v", a)
	}

	if !m.HandleKey("e") {
		t.Fatal("e refused")
	}
	m.form.setFallbackField(m.d.CLIs, "none", "")
	feed(m, "enter")
	a := rosterAgent(t, m, "scout")
	if a.FallbackEngine != manifest.FallbackNone || a.FallbackRuntime != "" || a.FallbackModel != "" {
		t.Fatalf("none not saved: %+v", a)
	}
	// "none" survives the boot-time default pass: it is a choice.
	if changed, err := org.EnsureFallbacks(m.d.WS); err != nil || len(changed) != 0 {
		t.Fatalf("EnsureFallbacks touched a deliberate none: %v %v", changed, err)
	}
}

// Picking the agent's own engine as its fallback is refused by name.
func TestAgentFallbackSameAsEngineRefused(t *testing.T) {
	withDefaultFallback(t, "claude", "codex")
	m, _, _, _ := agentSurface(t)
	if !m.HandleKey("n") {
		t.Fatal("n refused")
	}
	fillAgent(m, "spec", "Spec", "m-1")
	m.form.setFallbackField(m.d.CLIs, "claude", "")
	feed(m, "enter")
	if m.form.err == "" || !strings.Contains(m.form.err, "own engine") {
		t.Fatalf("err = %q", m.form.err)
	}
}

func TestAgentsListShowsFallback(t *testing.T) {
	withDefaultFallback(t, "claude", "codex")
	m, ws, o, _ := agentSurface(t)
	if err := o.CreateAgent(ws, &manifest.Agent{ID: "scout", Name: "Scout", Model: "m", Runtime: "claude"}); err != nil {
		t.Fatal(err)
	}
	if got := ansi.Strip(strings.Join(m.agentsView(), "\n")); !strings.Contains(got, "fallback codex") {
		t.Fatalf("roster does not show the default fallback:\n%s", got)
	}
}
