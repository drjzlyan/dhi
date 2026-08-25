package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/sandbox"
)

func validDoc() string {
	return `schema = 1
name = "Scout"
model = "claude-sonnet-4-5"
system = "You scout code."
tools = ["read", "list", "search", "mcp__docs__lookup"]
policy_json = """{"rules":[{"op":"read","effect":"allow"}]}"""
env_var = "ANTHROPIC_API_KEY"
`
}

func TestParseValid(t *testing.T) {
	a, err := Parse("scout", []byte(validDoc()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if a.ID != "scout" || a.Name != "Scout" || a.Model != "claude-sonnet-4-5" {
		t.Errorf("identity fields wrong: %+v", a)
	}
	if a.System != "You scout code." {
		t.Errorf("System = %q", a.System)
	}
	want := []string{"read", "list", "search", "mcp__docs__lookup"}
	if len(a.Tools) != len(want) {
		t.Fatalf("Tools = %v, want %v", a.Tools, want)
	}
	for i := range want {
		if a.Tools[i] != want[i] {
			t.Errorf("Tools[%d] = %q, want %q", i, a.Tools[i], want[i])
		}
	}
	if a.EnvVar != "ANTHROPIC_API_KEY" {
		t.Errorf("EnvVar = %q", a.EnvVar)
	}
	if a.Policy() == nil {
		t.Fatal("Policy() = nil, want parsed policy")
	}
	got := a.Policy().Evaluate(sandbox.OpRead, "anything")
	if got.Effect != sandbox.Allow {
		t.Errorf("policy read = %v, want allow", got.Effect)
	}
}

func TestParseMinimalDefaults(t *testing.T) {
	doc := "schema = 1\nname = \"Bare\"\nmodel = \"m\"\n"
	a, err := Parse("bare", []byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(a.Tools) != 0 {
		t.Errorf("Tools = %v, want empty (allow nothing by default)", a.Tools)
	}
	if a.Policy() != nil {
		t.Error("Policy() != nil with no policy_json")
	}
	if a.EnvVar != "" || a.System != "" {
		t.Errorf("defaults leaked: %+v", a)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		id   string
		doc  string
		want string
	}{
		{"bad id", "Big-Agent", "schema = 1\nname = \"n\"\nmodel = \"m\"\n", "bad agent id"},
		{"unknown key", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\ntemperament = \"calm\"\n", "unknown key"},
		{"bad schema", "a", "schema = 2\nname = \"n\"\nmodel = \"m\"\n", "schema 2"},
		{"missing name", "a", "schema = 1\nmodel = \"m\"\n", "name is required"},
		{"missing model", "a", "schema = 1\nname = \"n\"\n", "model is required"},
		{"dup tool", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\ntools = [\"read\", \"read\"]\n", "duplicate tool"},
		{"typo builtin", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\ntools = [\"serch\"]\n", "unknown tool"},
		{"bad mcp ref", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\ntools = [\"mcp_docs_lookup\"]\n", "unknown tool"},
		{"empty tool", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\ntools = [\" \"]\n", "unknown tool"},
		{"bad policy effect", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\npolicy_json = \"{\\\"rules\\\":[{\\\"op\\\":\\\"read\\\",\\\"effect\\\":\\\"maybe\\\"}]}\"\n", "policy_json"},
		{"policy not json", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\npolicy_json = \"{\"\n", "policy_json"},
		{"bad env var", "a", "schema = 1\nname = \"n\"\nmodel = \"m\"\nenv_var = \"9KEYS\"\n", "env_var"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.id, []byte(tc.doc))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, doc string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	minimal := func(name string) string {
		return "schema = 1\nname = \"" + name + "\"\nmodel = \"m\"\n"
	}
	write("zeta.toml", minimal("Z"))
	write("alpha.toml", minimal("A"))
	write("notes.md", "# not an agent")
	if err := os.Mkdir(filepath.Join(dir, "subdir.toml"), 0o755); err != nil {
		t.Fatal(err)
	}

	roster, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(roster) != 2 {
		t.Fatalf("roster = %d agents, want 2", len(roster))
	}
	if roster[0].ID != "alpha" || roster[1].ID != "zeta" {
		t.Errorf("roster not sorted by id: %s, %s", roster[0].ID, roster[1].ID)
	}
}

func TestLoadDirIDMismatch(t *testing.T) {
	dir := t.TempDir()
	doc := "schema = 1\nname = \"N\"\nmodel = \"m\"\n"
	if err := os.WriteFile(filepath.Join(dir, "other.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	// Parse is id-driven; LoadDir passes the filename stem. A stem that is
	// itself invalid must fail.
	if _, err := LoadDir(dir); err != nil {
		t.Fatalf("valid stem should load: %v", err)
	}
	bad := filepath.Join(dir, "Bad-ID.toml")
	if err := os.WriteFile(bad, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("invalid stem should fail")
	} else if !strings.Contains(err.Error(), "Bad-ID") {
		t.Errorf("error %q does not name the bad file", err)
	}
}

func TestLoadDirMissing(t *testing.T) {
	roster, err := LoadDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir: %v", err)
	}
	if len(roster) != 0 {
		t.Errorf("roster = %d, want 0", len(roster))
	}
}
