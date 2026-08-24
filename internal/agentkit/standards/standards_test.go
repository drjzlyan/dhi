package standards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func TestResolveBuiltinsOnly(t *testing.T) {
	root := t.TempDir()
	out := Resolve(root, "alice", nil)
	if !strings.Contains(out, "Standing instructions") ||
		!strings.Contains(out, "force-push") || !strings.Contains(out, "- Run the") {
		t.Fatalf("builtin block wrong:\n%s", out)
	}
}

func TestLayeredResolution(t *testing.T) {
	root := t.TempDir()
	doc := `schema = 1

workspace = ["use conventional commits"]

[teams.frontend]
extend = ["prefer table-driven tests"]

[teams.infra]
extend = ["never touch migrations"]

[agents.alice]
mode = "extend"
entries = ["sign off with commit hash"]

[agents.bob]
mode = "replace"
entries = ["only answer with diffs"]
`
	writeStandards(t, root, doc)

	frontendOnly := func(id string) []string {
		if id == "alice" {
			return []string{"frontend"}
		}
		if id == "bob" {
			return []string{"infra", "frontend"} // multi-team
		}
		return nil
	}

	a := Resolve(root, "alice", frontendOnly)
	for _, want := range []string{
		"conventional commits", "table-driven tests",
		"sign off with commit hash", "Run the project's tests",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("alice block missing %q:\n%s", want, a)
		}
	}
	if strings.Contains(a, "migrations") {
		t.Error("alice picked up non-member team layer")
	}

	b := Resolve(root, "bob", frontendOnly)
	if !strings.Contains(b, "only answer with diffs") {
		t.Errorf("bob block missing override:\n%s", b)
	}
	// Replace keeps builtins but discards workspace/team layers.
	if !strings.Contains(b, "Run the project's tests") {
		t.Error("replace must keep builtins")
	}
	for _, gone := range []string{"conventional commits", "table-driven", "migrations"} {
		if strings.Contains(b, gone) {
			t.Errorf("replace kept upstream layer %q", gone)
		}
	}

	// Teamless agent gets workspace+builtins only.
	c := Resolve(root, "carol", frontendOnly)
	if strings.Contains(c, "table-driven") || strings.Contains(c, "commit hash") {
		t.Errorf("carol leaked scoped layers:\n%s", c)
	}
}

func writeStandards(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, workspace.DHIDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, File), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidDocumentDegradesToBuiltins(t *testing.T) {
	root := t.TempDir()
	writeStandards(t, root, "schema = 99\n")
	out := Resolve(root, "x", nil)
	if !strings.Contains(out, "force-push") {
		t.Fatal("invalid doc did not degrade to builtins")
	}

	writeStandards(t, root, "schema = 1\nbogus = 1\n")
	out = Resolve(root, "x", nil)
	if !strings.Contains(out, "force-push") {
		t.Fatal("unknown-key doc did not degrade to builtins")
	}
}

func TestSaveRoundTripAndValidation(t *testing.T) {
	root := t.TempDir()
	err := Save(root,
		[]string{"use conventional commits"},
		map[string][]string{"frontend": {"prefer table-driven tests"}},
		map[string]AgentOverride{"alice": {Mode: ModeReplace, Entries: []string{"diffs only"}}},
	)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Resolve(root, "alice", func(string) []string { return []string{"frontend"} })
	if !strings.Contains(got, "diffs only") || !strings.Contains(got, "Run the project's tests") {
		t.Errorf("alice replace block wrong:\n%s", got)
	}
	if strings.Contains(got, "conventional commits") || strings.Contains(got, "table-driven") {
		t.Error("replace kept workspace/team layers")
	}

	carol := Resolve(root, "carol", func(id string) []string {
		if id == "carol" {
			return []string{"frontend"}
		}
		return nil
	})
	for _, want := range []string{"conventional commits", "table-driven", "Keep changes minimal"} {
		if !strings.Contains(carol, want) {
			t.Errorf("carol extend block missing %q:\n%s", want, carol)
		}
	}

	if err := Save(root, nil, map[string][]string{"Bad Slug": nil}, nil); err == nil {
		t.Error("bad team slug accepted")
	}
	if err := Save(root, nil, nil,
		map[string]AgentOverride{"a": {Mode: "nope"}}); err == nil {
		t.Error("bad mode accepted")
	}
}
