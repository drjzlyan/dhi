package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/workspace"
)

func setupOrg(t *testing.T) (*Org, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, workspace.DHIDir), 0o755); err != nil {
		t.Fatal(err)
	}
	o, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return o, root
}

func TestMissingFileIsEmptyOrg(t *testing.T) {
	o, _ := setupOrg(t)
	if len(o.Teams()) != 0 {
		t.Fatalf("fresh org has teams: %+v", o.Teams())
	}
}

func TestCreatePersistAndReload(t *testing.T) {
	o, root := setupOrg(t)
	if err := o.CreateTeam("frontend", "you", []string{"alice", "bob", "alice"}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	reloaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	tm, ok := reloaded.Team("frontend")
	if !ok {
		t.Fatal("team not persisted")
	}
	if tm.Lead != "you" {
		t.Errorf("lead = %q", tm.Lead)
	}
	if strings.Join(tm.Members, ",") != "alice,bob" {
		t.Errorf("members = %v (dedup+sort expected)", tm.Members)
	}
}

func TestValidationAndDuplicates(t *testing.T) {
	o, _ := setupOrg(t)
	cases := []struct {
		slug, lead string
		members    []string
		wantErr    string
	}{
		{"Bad Slug", "", nil, "bad team name"},
		{"ok", "Not Valid", nil, "bad lead"},
		{"ok", "", []string{"ok-id", "Bad!"}, "bad member"},
		{"ok", "", []string{"a"}, ""}, // legal
		{"ok", "", nil, "already exists"},
	}
	for _, tc := range cases {
		err := o.CreateTeam(tc.slug, tc.lead, tc.members)
		if tc.wantErr == "" && err != nil {
			t.Errorf("CreateTeam(%q): %v", tc.slug, err)
			continue
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("CreateTeam(%q,%q) = %v, want ~%q", tc.slug, tc.lead, err, tc.wantErr)
		}
	}
}

func TestUpdateAndDelete(t *testing.T) {
	o, root := setupOrg(t)
	if err := o.CreateTeam("core", "", []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	if err := o.UpdateTeam("core", "alice", []string{"alice", "bob"}); err != nil {
		t.Fatalf("UpdateTeam: %v", err)
	}
	if err := o.UpdateTeam("ghost", "", nil); err == nil || !strings.Contains(err.Error(), "unknown team") {
		t.Errorf("update unknown = %v", err)
	}

	ch, cancel := o.Subscribe()
	defer cancel()
	if err := o.DeleteTeam("core"); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ch:
		if c.Kind != TeamRemoved || c.Team != "core" {
			t.Fatalf("event = %+v", c)
		}
	default:
		t.Fatal("no delete event")
	}

	reloaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Teams()) != 0 {
		t.Fatalf("delete not persisted: %+v", reloaded.Teams())
	}
	if err := o.DeleteTeam("core"); err == nil || !strings.Contains(err.Error(), "unknown team") {
		t.Errorf("double delete = %v", err)
	}
}

func TestTeamsOfMembershipLookup(t *testing.T) {
	o, _ := setupOrg(t)
	if err := o.CreateTeam("frontend", "", []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	if err := o.CreateTeam("infra", "", []string{"alice", "zoe"}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(o.TeamsOf("alice"), ",")
	if got != "frontend,infra" {
		t.Errorf("TeamsOf(alice) = %q", got)
	}
	if v := o.TeamsOf("nobody"); len(v) != 0 {
		t.Errorf("TeamsOf(nobody) = %v", v)
	}
}

func TestStrictUnknownKeysRejected(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, workspace.DHIDir), 0o755)
	path := filepath.Join(root, File)
	os.WriteFile(path, []byte("schema = 1\n\n[teams.x]\nsecret = true\n"), 0o644)
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unknown keys accepted: %v", err)
	}
}

func TestConcurrentReadersAndWriters(t *testing.T) {
	o, _ := setupOrg(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			slug := "t" + string(rune('a'+i%26)) + string(rune('0'+i/26))
			_ = o.CreateTeam(slug, Human, []string{"a"})
			_ = o.Teams()
			_ = o.TeamsOf("a")
		}
	}()
	for i := 0; i < 200; i++ {
		_ = o.TeamsOf("zz")
	}
	<-done
}
