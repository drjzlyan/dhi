package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentkitStandards "github.com/drjzlyan/dhi/internal/agentkit/standards"
)

func statusOf(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

// lockedTool mirrors toolchain.LockedTool's JSON shape; writing the file
// directly keeps the doctor tested against its on-disk contract.
type lockedTool struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Path    string `json:"path"`
}

func writeLockfile(t *testing.T, root string, tools map[string]lockedTool) {
	t.Helper()
	doc := struct {
		Schema int                   `json:"schema"`
		Tools  map[string]lockedTool `json:"tools"`
	}{Schema: 1, Tools: tools}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lock.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestToolchainFreshPrefix(t *testing.T) {
	checks := Toolchain(filepath.Join(t.TempDir(), "absent"))
	c, ok := statusOf(checks, "toolchain/prefix")
	if !ok || c.Status != Warn || !strings.Contains(c.Detail, "not installed yet") {
		t.Errorf("fresh prefix = %+v (found=%v)", c, ok)
	}
}

func TestToolchainHealthyInstall(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "tools", "rg", "14.1.0")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLockfile(t, root, map[string]lockedTool{
		"rg": {Version: "14.1.0", SHA256: strings.Repeat("0", 64), Path: filepath.Join("tools", "rg", "14.1.0")},
	})

	checks := Toolchain(root)
	if c, ok := statusOf(checks, "toolchain/prefix"); !ok || c.Status != OK {
		t.Errorf("prefix = %+v (found=%v)", c, ok)
	}
	if c, ok := statusOf(checks, "toolchain/rg"); !ok || c.Status != OK {
		t.Errorf("rg = %+v (found=%v)", c, ok)
	}
	if _, found := statusOf(checks, "toolchain/staging"); found {
		t.Error("staging check emitted without leftovers")
	}
}

func TestToolchainMissingPayloadFails(t *testing.T) {
	root := t.TempDir()
	writeLockfile(t, root, map[string]lockedTool{
		"rg": {Version: "14.1.0", SHA256: strings.Repeat("0", 64), Path: filepath.Join("tools", "rg", "14.1.0")},
	})
	checks := Toolchain(root)
	c, ok := statusOf(checks, "toolchain/rg")
	if !ok || c.Status != Fail {
		t.Errorf("missing payload = %+v (found=%v)", c, ok)
	}
}

func TestToolchainStagingLeftoversWarn(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging", "rg-14.1.0-xyz")
	os.MkdirAll(staging, 0o755)
	checks := Toolchain(root)
	c, ok := statusOf(checks, "toolchain/staging")
	if !ok || c.Status != Warn {
		t.Errorf("staging leftovers = %+v (found=%v)", c, ok)
	}
}

func TestToolchainCorruptLockfileFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lock.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := Toolchain(root)
	c, ok := statusOf(checks, "toolchain/lockfile")
	if !ok || c.Status != Fail {
		t.Errorf("corrupt lockfile = %+v (found=%v)", c, ok)
	}
}

func setupWorkspaceRoot(t *testing.T, reserveAll bool) string {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema = 1\n\n[members.main]\npath = \"repo\"\n"
	if err := os.WriteFile(filepath.Join(root, ".dhi", "workspace.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if reserveAll {
		for _, dir := range []string{".dhi/agents", ".dhi/memory", ".dhi/knowledge", ".dhi/channels", ".dhi/tasks"} {
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func TestWorkspaceChecks(t *testing.T) {
	root := setupWorkspaceRoot(t, false)

	checks := Workspace(root)
	c, ok := statusOf(checks, "workspace/config")
	if !ok || c.Status != OK || !strings.Contains(c.Detail, "1 member") {
		t.Fatalf("config = %+v (found=%v)", c, ok)
	}
	if _, found := statusOf(checks, "workspace/memory"); !found {
		t.Error("reserved-dir warning missing for absent .dhi/memory")
	}

	checks = Workspace(setupWorkspaceRoot(t, true))
	if len(checks) != 1 {
		t.Errorf("fully reserved workspace should emit only config check, got %+v", checks)
	}

	checks = Workspace(t.TempDir())
	if len(checks) != 1 || checks[0].Status != Warn {
		t.Errorf("non-workspace = %+v", checks)
	}
}

func TestRunAggregatesHealth(t *testing.T) {
	corrupt := t.TempDir()
	if err := os.WriteFile(filepath.Join(corrupt, "lock.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Run(corrupt, setupWorkspaceRoot(t, true))
	if r.Healthy {
		t.Error("corrupt lockfile reported healthy")
	}
	for _, c := range r.Checks {
		if c.Status == Fail && strings.HasPrefix(c.Name, "workspace/") {
			t.Errorf("healthy workspace reported failing: %+v", c)
		}
	}

	data, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("JSON invalid: %v\n%s", err, data)
	}
}

func TestStandardsSuite(t *testing.T) {
	ws := setupWorkspaceRoot(t, true)

	// Absent document: healthy defaults-only.
	checks := Standards(ws)
	if c, ok := statusOf(checks, "standards/config"); !ok || c.Status != OK {
		t.Fatalf("absent = %+v (found=%v)", c, ok)
	}

	// Custom layers referencing real roster + org entries pass.
	os.WriteFile(filepath.Join(ws, ".dhi", "agents", "alice.toml"),
		[]byte("schema = 1\nname = \"Alice\"\nmodel = \"m\"\n"), 0o644)
	os.WriteFile(filepath.Join(ws, ".dhi", "org.toml"),
		[]byte("schema = 1\n\n[teams.frontend]\nmembers = [\"alice\"]\n"), 0o644)
	if err := agentkitStandards.Save(ws,
		[]string{"use conventional commits"},
		map[string][]string{"frontend": {"prefer table-driven tests"}},
		map[string]agentkitStandards.AgentOverride{"alice": {Mode: "extend", Entries: []string{"be terse"}}},
	); err != nil {
		t.Fatal(err)
	}
	checks = Standards(ws)
	c, ok := statusOf(checks, "standards/config")
	if !ok || c.Status != OK || !strings.Contains(c.Detail, "1 workspace") {
		t.Fatalf("healthy layers = %+v (found=%v)", c, ok)
	}

	// Typos warn with specifics.
	agentkitStandards.Save(ws, nil,
		map[string][]string{"frontened": nil}, // typo'd team
		map[string]agentkitStandards.AgentOverride{"ghost": {Mode: "extend"}})
	checks = Standards(ws)
	c, _ = statusOf(checks, "standards/config")
	if c.Status != Warn ||
		!strings.Contains(c.Detail, "team frontened") ||
		!strings.Contains(c.Detail, "agent ghost") {
		t.Fatalf("typo warnings = %+v", c)
	}

	// Malformed doc warns about fallback.
	os.WriteFile(filepath.Join(ws, ".dhi", "standards.toml"), []byte("schema = 7\n"), 0o644)
	checks = Standards(ws)
	c, _ = statusOf(checks, "standards/config")
	if c.Status != Warn || !strings.Contains(c.Detail, "built-ins") {
		t.Fatalf("malformed = %+v", c)
	}
}
