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

func writeAgent(t *testing.T, root, id, runtime, tools string) {
	t.Helper()
	doc := "schema = 1\nname = \"S\"\nmodel = \"m\"\nruntime = \"" + runtime + "\"\ntools = [" + tools + "]\n"
	if err := os.WriteFile(filepath.Join(root, workspace.DirAgents, id+".toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAgentToolsIdle pins the single ok row when no agent allowlists a
// served tool.
func TestAgentToolsIdle(t *testing.T) {
	root := wsFixture(t)
	// git_push is not a served slug (the fs + git tools are, as of M15 P1).
	writeAgent(t, root, "scout", "claude", `"git_push"`)
	got := AgentTools(root)
	if len(got) != 1 || got[0].Name != "agent-tools" || got[0].Status != OK {
		t.Fatalf("checks = %+v", got)
	}
	if !strings.Contains(got[0].Detail, "idle") {
		t.Errorf("detail = %q, want idle", got[0].Detail)
	}
}

// TestAgentToolsServingOnMCPAdapter pins the ok row naming the agent
// and adapter when the allowlist intersects the served set.
func TestAgentToolsServingOnMCPAdapter(t *testing.T) {
	root := wsFixture(t)
	writeAgent(t, root, "scout", "claude", `"memory_append","task_list"`)
	got := AgentTools(root)
	if len(got) != 1 || got[0].Status != OK {
		t.Fatalf("checks = %+v", got)
	}
	if !strings.Contains(got[0].Detail, "scout (claude, 2 tool(s))") {
		t.Errorf("detail = %q", got[0].Detail)
	}
}

// TestAgentToolsNoEngineWarns pins the named warn: a served allowlist
// with no engine and no workspace default cannot be served.
func TestAgentToolsNoEngineWarns(t *testing.T) {
	root := wsFixture(t)
	doc := "schema = 3\nname = \"S\"\nmodel = \"m\"\ntools = [\"memory_append\"]\n"
	if err := os.WriteFile(filepath.Join(root, workspace.DirAgents, "scout.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := AgentTools(root)
	if len(got) != 1 || got[0].Name != "agent-tools" {
		t.Fatalf("checks = %+v, want one agent-tools row", got)
	}
	if got[0].Status != Warn || !strings.Contains(got[0].Detail, "scout") ||
		!strings.Contains(got[0].Detail, "no engine") {
		t.Fatalf("warn row = %+v", got[0])
	}
}

func TestAuthorityReportsOverrides(t *testing.T) {
	root := wsFixture(t)
	doc := "schema = 4\nname = \"S\"\nmodel = \"m\"\nengine = \"cli:claude\"\n[scopes]\nwrite = \"deny\"\n"
	if err := os.WriteFile(filepath.Join(root, workspace.DirAgents, "scout.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	c, ok := statusOf(Authority(root), "authority")
	if !ok || c.Status != OK || !strings.Contains(c.Detail, "scout: write=deny") {
		t.Fatalf("authority = %+v (found=%v)", c, ok)
	}
}

func TestAuthorityDefaults(t *testing.T) {
	root := wsFixture(t)
	writeAgent(t, root, "scout", "claude", "")
	c, ok := statusOf(Authority(root), "authority")
	if !ok || !strings.Contains(c.Detail, "default capability scopes") {
		t.Fatalf("authority = %+v (found=%v)", c, ok)
	}
}

func TestWorkflowsDoctor(t *testing.T) {
	root := wsFixture(t)

	c, ok := statusOf(Workflows(root), "workflows")
	if !ok || c.Status != OK {
		t.Fatalf("default workflows row = %+v (found=%v)", c, ok)
	}

	// A dangling active workflow warns by name.
	doc := "schema = 5\nname = \"S\"\nmodel = \"m\"\nengine = \"cli:claude\"\nworkflow = \"ghost\"\n"
	if err := os.WriteFile(filepath.Join(root, workspace.DirAgents, "scout.toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ = statusOf(Workflows(root), "workflows")
	if c.Status != Warn || !strings.Contains(c.Detail, "ghost") {
		t.Fatalf("dangling workflow row = %+v", c)
	}

	// A malformed definition fails by name.
	dir := filepath.Join(root, workspace.DHIDir, "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("schema = 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ = statusOf(Workflows(root), "workflows")
	if c.Status != Fail || !strings.Contains(c.Detail, "broken") {
		t.Fatalf("malformed workflow row = %+v", c)
	}
}

func TestDependenciesDoctor(t *testing.T) {
	root := wsFixture(t)
	if c, ok := statusOf(Dependencies(root), "dependencies"); !ok || c.Status != OK {
		t.Fatalf("default dependencies row = %+v", c)
	}
	// Declare an edge to a member that does not exist → Warn by name.
	doc := "schema = 1\n\n[members.api]\npath = \"api\"\n\n[[dependency]]\nfrom = \"api\"\nto = \"ghost\"\nkind = \"api\"\n"
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	c, ok := statusOf(Dependencies(root), "dependencies")
	if !ok || c.Status != Warn || !strings.Contains(c.Detail, "ghost") {
		t.Fatalf("dangling row = %+v", c)
	}
}

func TestLibraryCheckNamesDanglingReferences(t *testing.T) {
	root := wsFixture(t)
	if got := Library(root); len(got) != 0 {
		t.Fatalf("no roster should add no check: %+v", got)
	}
	dir := filepath.Join(root, workspace.DirAgents)
	good := "schema = 6\nname = \"G\"\nmodel = \"m\"\nengine = \"cli:claude\"\nrole = \"fixer\"\nskills = [\"docs\"]\npersona = \"mentor\"\n"
	os.WriteFile(filepath.Join(dir, "good.toml"), []byte(good), 0o644)
	c, ok := statusOf(Library(root), "agents/library")
	if !ok || c.Status != OK {
		t.Fatalf("all-resolving roster = %+v", c)
	}

	bad := "schema = 6\nname = \"B\"\nmodel = \"m\"\nengine = \"cli:claude\"\nrole = \"wizard\"\nskills = [\"nope\"]\npersona = \"grumpy\"\n"
	os.WriteFile(filepath.Join(dir, "bad.toml"), []byte(bad), 0o644)
	os.WriteFile(filepath.Join(root, workspace.DirPersonas, "broken.toml"), []byte("schema = 1\n"), 0o644)
	c, _ = statusOf(Library(root), "agents/library")
	if c.Status != Warn {
		t.Fatalf("status = %v", c.Status)
	}
	for _, want := range []string{"bad: role wizard", "bad: skill nope", "bad: persona grumpy", "broken.toml"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail lacks %q: %s", want, c.Detail)
		}
	}
	if strings.Contains(c.Detail, "good:") {
		t.Errorf("resolving agent flagged: %s", c.Detail)
	}
}
