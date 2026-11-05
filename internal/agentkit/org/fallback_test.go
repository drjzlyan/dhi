package org

import (
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
)

func withPicker(t *testing.T, installed ...string) {
	t.Helper()
	prev := DefaultFallback
	t.Cleanup(func() { DefaultFallback = prev })
	DefaultFallback = func(primary string) (string, string) { return clirun.PickFallback(primary, installed) }
}

// F-180: a new agent starts with the default fallback written into its
// manifest, so it is visible; a recorded choice is never overwritten.
func TestCreateAgentRecordsDefaultFallback(t *testing.T) {
	withPicker(t, "claude", "codex")
	o, ws := setupCrew(t)
	a := testAgent("alice", "Alice") // engine claude
	if err := o.CreateAgent(ws, a); err != nil {
		t.Fatal(err)
	}
	roster, _ := LoadRoster(ws)
	if roster[0].FallbackEngine != "cli:codex" {
		t.Fatalf("fallback = %q, want cli:codex", roster[0].FallbackEngine)
	}

	none := testAgent("bob", "Bob")
	none.FallbackEngine = manifest.FallbackNone
	if err := o.CreateAgent(ws, none); err != nil {
		t.Fatal(err)
	}
	roster, _ = LoadRoster(ws)
	for _, r := range roster {
		if r.ID == "bob" && r.FallbackEngine != manifest.FallbackNone {
			t.Fatalf("a deliberate none was replaced: %q", r.FallbackEngine)
		}
	}
}

func TestApplyFallbackNoCandidateLeavesItUnsetWithANote(t *testing.T) {
	withPicker(t, "claude")
	a := testAgent("alice", "Alice")
	note := ApplyFallback(a)
	if a.FallbackEngine != "" || note == "" {
		t.Fatalf("fallback = %q note = %q", a.FallbackEngine, note)
	}
}

func TestEnsureFallbacksFillsOnlyUnsetAgents(t *testing.T) {
	o, ws := setupCrew(t)
	// Written before the picker exists: these agents have none recorded.
	for _, id := range []string{"alice", "carol"} {
		if err := o.CreateAgent(ws, testAgent(id, id)); err != nil {
			t.Fatal(err)
		}
	}
	chosen := testAgent("dave", "Dave")
	chosen.FallbackEngine, chosen.FallbackRuntime = "cli:opencode", "opencode"
	if err := o.CreateAgent(ws, chosen); err != nil {
		t.Fatal(err)
	}

	withPicker(t, "claude", "codex", "opencode")
	changed, err := EnsureFallbacks(ws)
	if err != nil || len(changed) != 2 || changed[0] != "alice" || changed[1] != "carol" {
		t.Fatalf("changed = %v err = %v", changed, err)
	}
	roster, _ := LoadRoster(ws)
	for _, a := range roster {
		want := "cli:codex"
		if a.ID == "dave" {
			want = "cli:opencode"
		}
		if a.FallbackEngine != want {
			t.Errorf("%s fallback = %q, want %q", a.ID, a.FallbackEngine, want)
		}
	}
	if again, _ := EnsureFallbacks(ws); len(again) != 0 {
		t.Fatalf("second pass changed %v", again)
	}
}

func TestEnsureFallbacksNoOtherCLIChangesNothing(t *testing.T) {
	o, ws := setupCrew(t)
	if err := o.CreateAgent(ws, testAgent("alice", "Alice")); err != nil {
		t.Fatal(err)
	}
	withPicker(t, "claude")
	if changed, err := EnsureFallbacks(ws); err != nil || len(changed) != 0 {
		t.Fatalf("changed = %v err = %v", changed, err)
	}
}
