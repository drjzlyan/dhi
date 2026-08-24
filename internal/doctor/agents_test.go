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

func TestAgentsValidRosterAndMissingKey(t *testing.T) {
	root := wsFixture(t)
	dir := filepath.Join(root, workspace.DirAgents)
	doc := "schema = 1\nname = \"S\"\nmodel = \"m\"\nenv_var = \"DHI_TEST_KEY\"\n"
	if err := os.WriteFile(filepath.Join(dir, "scout.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Agents(root)
	if len(got) != 2 {
		t.Fatalf("checks = %+v", got)
	}
	if got[0].Status != OK || !strings.Contains(got[0].Detail, "1 agent(s): scout") {
		t.Errorf("roster check = %+v", got[0])
	}
	if got[1].Status != Warn || !strings.Contains(got[1].Detail, "DHI_TEST_KEY") {
		t.Errorf("env check = %+v", got[1])
	}

	t.Setenv("DHI_TEST_KEY", "x")
	got = Agents(root)
	if len(got) != 1 || got[0].Status != OK {
		t.Errorf("with key set: %+v", got)
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
