package behavior

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
)

func TestComposeOrder(t *testing.T) {
	out := Compose(Input{
		AgentID:        "scout",
		Workspace:      "dhi",
		ManifestSystem: "manifest line",
		Role:           &library.Role{System: "role for {{agent}} at {{workspace}}"},
		Skills: []library.Skill{
			{Slug: "a", Body: "skill A"},
			{Slug: "b", Body: "skill B"},
		},
	})
	want := "manifest line\n\nrole for scout at dhi\n\nskill A\n\nskill B"
	if out != want {
		t.Fatalf("compose = %q, want %q", out, want)
	}
}

func TestComposeSkipsEmptyParts(t *testing.T) {
	out := Compose(Input{ManifestSystem: "  ", Role: &library.Role{System: "  "}})
	if out != "" {
		t.Fatalf("empty compose = %q", out)
	}
	out = Compose(Input{ManifestSystem: "only manifest"})
	if out != "only manifest" {
		t.Fatalf("manifest-only = %q", out)
	}
}

func TestResolveDropsDanglingRefs(t *testing.T) {
	m := &manifest.Agent{Role: "missing", Skills: []string{"nope", "real"}}
	// A library where only "real" exists (builtins provide none).
	lib := library.Open(nil)
	role, skills := Resolve(m, lib)
	if role != nil {
		t.Fatalf("dangling role resolved: %+v", role)
	}
	if len(skills) != 0 {
		t.Fatalf("dangling skills resolved: %+v", skills)
	}
	// A manifest with no library stays total.
	role, skills = Resolve(m, nil)
	if role != nil || skills != nil {
		t.Fatal("nil library not total")
	}
	_ = strings.TrimSpace("")
}
