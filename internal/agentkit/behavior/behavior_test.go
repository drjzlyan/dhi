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
			{Slug: "a", Description: "does A", Body: "skill A body"},
			{Slug: "b", Description: "does B", Body: "skill B body", Dir: "/skills/b"},
		},
	})
	if !strings.HasPrefix(out, "manifest line\n\nrole for scout at dhi\n\n") {
		t.Fatalf("manifest then role first: %q", out)
	}
	// ADR-0043: skills are a catalog in manifest order, not inlined bodies.
	if strings.Contains(out, "skill A body") {
		t.Fatal("a skill body must not ride in the prompt")
	}
	a, b := strings.Index(out, "<name>a</name>"), strings.Index(out, "<name>b</name>")
	if a < 0 || b < a || !strings.Contains(out, "<location>/skills/b/SKILL.md</location>") {
		t.Fatalf("catalog: %q", out)
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

func TestComposeOrdersPersonaBetweenRoleAndSkills(t *testing.T) {
	out := Compose(Input{
		AgentID: "forge", Workspace: "acme",
		ManifestSystem: "MANIFEST",
		Role:           &library.Role{System: "ROLE for {{agent}}"},
		Persona:        &library.Persona{Tone: "calm", Verbosity: library.VerbosityTerse},
		Skills:         []library.Skill{{Body: "SKILL"}},
	})
	order := []string{"MANIFEST", "ROLE for forge", "Voice: calm.", "SKILL"}
	last := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < 0 || i <= last {
			t.Fatalf("%q out of order (at %d, previous %d) in:\n%s", want, i, last, out)
		}
		last = i
	}
	if strings.Contains(Compose(Input{ManifestSystem: "x", Persona: &library.Persona{}}), "Voice") {
		t.Fatal("an empty persona must add no block")
	}
}
