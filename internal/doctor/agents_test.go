package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func wsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "api"), 0o755)
	if err := workspace.Create(root, "api"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAgentsNoRosterIsNoCheck(t *testing.T) {
	root := wsFixture(t)
	if got := Agents(root); len(got) != 0 {
		t.Errorf("checks = %+v, want none", got)
	}
}

func TestAgentsValidRoster(t *testing.T) {
	root := wsFixture(t)
	dir := filepath.Join(root, workspace.DirAgents)
	doc := "schema = 1\nname = \"S\"\nmodel = \"m\"\nruntime = \"claude\"\n"
	if err := os.WriteFile(filepath.Join(dir, "scout.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Agents(root)
	if len(got) != 1 {
		t.Fatalf("checks = %+v", got)
	}
	if got[0].Status != OK || !strings.Contains(got[0].Detail, "1 agent(s): scout") {
		t.Errorf("roster check = %+v", got[0])
	}
}

func TestAgentsMissingRuntimeFails(t *testing.T) {
	// ADR-0013: every rostered agent must name a registered host CLI;
	// a manifest without `runtime` is a parse failure now, not a
	// fallback to an in-house engine.
	root := wsFixture(t)
	dir := filepath.Join(root, workspace.DirAgents)
	doc := "schema = 1\nname = \"S\"\nmodel = \"m\"\n"
	if err := os.WriteFile(filepath.Join(dir, "scout.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Agents(root)
	if len(got) != 1 || got[0].Status != Fail {
		t.Fatalf("checks = %+v, want single fail", got)
	}
}

func TestAgentsBrokenManifestFails(t *testing.T) {
	root := wsFixture(t)
	dir := filepath.Join(root, workspace.DirAgents)
	if err := os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("schema = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Agents(root)
	if len(got) != 1 || got[0].Status != Fail {
		t.Fatalf("checks = %+v, want single fail", got)
	}
}
