package pack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/workspace"
)

const aliceDoc = `schema = 1
name = "Alice"
model = "mock-1"
tools = ["read", "list"]
`

const bobDoc = `schema = 1
name = "Bob"
model = "mock-1"
tools = ["read"]
`

// fixturePack writes a valid pack into a temp dir and returns its root.
func fixturePack(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	agents := filepath.Join(root, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(agents, "alice.toml"), []byte(aliceDoc), 0o644)
	os.WriteFile(filepath.Join(agents, "bob.toml"), []byte(bobDoc), 0o644)
	spec := "schema = 1\nname = \"" + name + "\"\nversion = \"0.1.0\"\n" +
		"description = \"test crew\"\nagents = [\"agents/bob.toml\", \"agents/alice.toml\"]\n"
	os.WriteFile(filepath.Join(root, "pack.toml"), []byte(spec), 0o644)
	return root
}

func setupWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "main"), 0o755)
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestInstallFromLocalDir(t *testing.T) {
	ws := setupWS(t)
	in := &Installer{WS: ws}
	res, err := in.Install(context.Background(), fixturePack(t, "acme"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Pack != "acme" || strings.Join(res.Agents, ",") != "alice,bob" {
		t.Fatalf("result = %+v", res)
	}
	roster, err := manifest.LoadDir(filepath.Join(ws.Root, workspace.DirAgents))
	if err != nil || len(roster) != 2 {
		t.Fatalf("roster = %+v err=%v", roster, err)
	}
	names, err := in.Installed()
	if err != nil || len(names) != 1 || names[0] != "acme" {
		t.Fatalf("Installed = %v err=%v", names, err)
	}
}

func TestReinstallSamePackUpdates(t *testing.T) {
	ws := setupWS(t)
	in := &Installer{WS: ws}
	src := fixturePack(t, "acme")
	if _, err := in.Install(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	// Bump Alice's name in the source and reinstall.
	updated := strings.Replace(aliceDoc, "Alice", "Alice v2", 1)
	os.WriteFile(filepath.Join(src, "agents", "alice.toml"), []byte(updated), 0o644)
	res, err := in.Install(context.Background(), src)
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if !res.Updated {
		t.Error("reinstall not flagged as update")
	}
	data, _ := os.ReadFile(filepath.Join(ws.Root, workspace.DirAgents, "alice.toml"))
	if !strings.Contains(string(data), "Alice v2") {
		t.Error("update did not land on disk")
	}
}

func TestConflictAbortsWithoutSideEffects(t *testing.T) {
	ws := setupWS(t)
	dir := filepath.Join(ws.Root, workspace.DirAgents)
	os.MkdirAll(dir, 0o755)

	// A hand-written alice exists.
	hand := &manifest.Agent{ID: "alice", Name: "Handmade", Model: "m"}
	if err := manifest.WriteFile(dir, hand); err != nil {
		t.Fatal(err)
	}
	in := &Installer{WS: ws}
	before, _ := os.ReadFile(filepath.Join(dir, "alice.toml"))
	_, err := in.Install(context.Background(), fixturePack(t, "other"))
	if err == nil || !strings.Contains(err.Error(), "another source") {
		t.Fatalf("conflict install = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "alice.toml"))
	if string(before) != string(after) {
		t.Error("conflicted install modified the foreign file")
	}
	if names, _ := in.Installed(); len(names) != 0 {
		t.Errorf("provenance written despite conflict: %v", names)
	}
}

func TestUninstallRemovesExactlyRecordedAgents(t *testing.T) {
	ws := setupWS(t)
	in := &Installer{WS: ws}
	src := fixturePack(t, "acme")
	if _, err := in.Install(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	// A manual agent that must survive uninstall.
	dir := filepath.Join(ws.Root, workspace.DirAgents)
	manual := &manifest.Agent{ID: "zoe", Name: "Zoe", Model: "m"}
	manifest.WriteFile(dir, manual)

	if err := in.Uninstall("acme"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	for _, id := range []string{"alice", "bob"} {
		if _, err := os.Stat(filepath.Join(dir, id+".toml")); !os.IsNotExist(err) {
			t.Errorf("%s survived uninstall", id)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "zoe.toml")); err != nil {
		t.Error("manual agent was removed by pack uninstall")
	}
	if names, _ := in.Installed(); len(names) != 0 {
		t.Errorf("provenance entry kept: %v", names)
	}
	if err := in.Uninstall("acme"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("double uninstall = %v", err)
	}
}

func TestSpecValidation(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "pack.toml"),
		[]byte("schema = 1\nname = \"x\"\nagents = [\"a.toml\"]\nfuture_key = true\n"), 0o644)
	if _, err := ReadSpec(root); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("unknown key accepted: %v", err)
	}

	os.WriteFile(filepath.Join(root, "pack.toml"), []byte("schema = 9\n"), 0o644)
	if _, err := ReadSpec(root); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Errorf("bad schema accepted: %v", err)
	}
}

func TestInstallFromGitLocalPath(t *testing.T) {
	// Serve the pack from a local git repo; Clone over a plain path uses
	// go-git's filesystem transport — no network.
	src := t.TempDir()
	fixturePack(t, "fromgit") // ensure helper ran for parity of contents
	repoDir := filepath.Join(src, "repo")
	os.MkdirAll(repoDir, 0o755)
	copyPackInto := func() {
		packRoot := filepath.Join(src, "..", filepath.Base(src))
		_ = packRoot
	}
	copyPackInto()

	// Build pack files directly inside repoDir.
	agents := filepath.Join(repoDir, "agents")
	os.MkdirAll(agents, 0o755)
	os.WriteFile(filepath.Join(agents, "alice.toml"), []byte(aliceDoc), 0o644)
	os.WriteFile(filepath.Join(repoDir, "pack.toml"),
		[]byte("schema = 1\nname = \"fromgit\"\nversion = \"1.0\"\nagents = [\"agents/alice.toml\"]\n"), 0o644)

	r, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("pack", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@local", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	ws := setupWS(t)
	in := &Installer{WS: ws}
	res, err := in.Install(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("Install(git path): %v", err)
	}
	if strings.Join(res.Agents, ",") != "alice" {
		t.Fatalf("result = %+v", res)
	}
}
