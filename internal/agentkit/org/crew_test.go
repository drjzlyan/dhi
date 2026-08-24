package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/workspace"
)

func testAgent(id, name string) *manifest.Agent {
	return &manifest.Agent{
		ID: id, Name: name, Model: "claude-smoke-1",
		System: "be brief", Tools: []string{"read", "list"}, EnvVar: "",
	}
}

func setupCrew(t *testing.T) (*Org, *workspace.Workspace) {
	t.Helper()
	o, root := setupOrg(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(root, "main"); err != nil {
		t.Fatal(err)
	}
	// Create refuses to overwrite; point members at the real repo.
	cfgPath := filepath.Join(root, workspace.ConfigFile)
	os.WriteFile(cfgPath, []byte("schema = 1\n\n[members.main]\npath = \"repo\"\n"), 0o644)
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return o, ws
}

func TestCreateUpdateArchiveRestoreLifecycle(t *testing.T) {
	o, ws := setupCrew(t)

	if err := o.CreateAgent(ws, testAgent("alice", "Alice")); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := o.CreateAgent(ws, testAgent("alice", "Dup")); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Errorf("dup create = %v", err)
	}

	roster, err := LoadRoster(ws)
	if err != nil || len(roster) != 1 || roster[0].ID != "alice" {
		t.Fatalf("roster after create = %+v err=%v", roster, err)
	}

	upd := testAgent("alice", "Alice v2")
	upd.Tools = []string{"read", "write"}
	if err := o.UpdateAgent(ws, upd); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	roster, _ = LoadRoster(ws)
	if roster[0].Name != "Alice v2" {
		t.Fatalf("update lost: %+v", roster[0])
	}

	if err := o.ArchiveAgent(ws, "alice"); err != nil {
		t.Fatalf("ArchiveAgent: %v", err)
	}
	if roster, _ = LoadRoster(ws); len(roster) != 0 {
		t.Fatalf("archived agent still active: %+v", roster)
	}
	if got := o.Archived(ws); len(got) != 1 || got[0] != "alice" {
		t.Fatalf("Archived = %v", got)
	}
	back, err := manifest.ReadArchived(RosterDir(ws), "alice")
	if err != nil || back.Name != "Alice v2" {
		t.Fatalf("ReadArchived = %+v err=%v", back, err)
	}

	if err := o.RestoreAgent(ws, "alice"); err != nil {
		t.Fatalf("RestoreAgent: %v", err)
	}
	if roster, _ = LoadRoster(ws); len(roster) != 1 {
		t.Fatalf("restore failed: %+v", roster)
	}
	if err := o.RestoreAgent(ws, "alice"); err == nil ||
		!strings.Contains(err.Error(), "already active") {
		t.Errorf("double restore = %v", err)
	}
}

func TestCreateRejectsArchivedID(t *testing.T) {
	o, ws := setupCrew(t)
	if err := o.CreateAgent(ws, testAgent("bob", "Bob")); err != nil {
		t.Fatal(err)
	}
	if err := o.ArchiveAgent(ws, "bob"); err != nil {
		t.Fatal(err)
	}
	err := o.CreateAgent(ws, testAgent("bob", "Bob2"))
	if err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("create over archived = %v", err)
	}
}
