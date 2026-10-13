package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-090: an edit writes only the changed key to the chosen layer; the
// user layer's other values are never copied into the workspace.
func TestSaveChangedWritesOnlyTheEdit(t *testing.T) {
	dir := t.TempDir()
	user, ws := filepath.Join(dir, "user.toml"), filepath.Join(dir, "ws.toml")
	if err := os.WriteFile(user, []byte("schema = 1\ntheme = \"light-paper\"\n[agents]\nmax_turns = 40\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	before := Snapshot(c)
	c.Agents.MaxTurns = 12
	keys, err := SaveChanged(ws, before, c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, ",") != "agents.max_turns" {
		t.Fatalf("changed = %v", keys)
	}
	b, _ := os.ReadFile(ws)
	if strings.Contains(string(b), "light-paper") || !strings.Contains(string(b), "max_turns = 12") {
		t.Fatalf("workspace layer:\n%s", b)
	}
	// The user layer still governs everything else.
	if err := os.WriteFile(user, []byte("schema = 1\ntheme = \"high-contrast\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(user, ws)
	if err != nil || c2.Theme != "high-contrast" || c2.Agents.MaxTurns != 12 {
		t.Fatalf("reloaded theme=%q turns=%d err=%v", c2.Theme, c2.Agents.MaxTurns, err)
	}
	if !LayerHas(ws, "agents.max_turns") || LayerHas(ws, "theme") {
		t.Fatal("LayerHas wrong")
	}
}

// The repair finds what an older DHI copied: values equal to the user's.
func TestDuplicateKeysFindsCopiedValues(t *testing.T) {
	dir := t.TempDir()
	user, ws := filepath.Join(dir, "user.toml"), filepath.Join(dir, "ws.toml")
	_ = os.WriteFile(user, []byte("schema = 1\ntheme = \"light-paper\"\n[agents]\nmax_turns = 40\n"), 0o644)
	_ = os.WriteFile(ws, []byte("schema = 1\ntheme = \"light-paper\"\n[agents]\nmax_turns = 12\n"), 0o644)
	dup, err := DuplicateKeys(user, ws)
	if err != nil || strings.Join(dup, ",") != "theme" {
		t.Fatalf("dup = %v %v", dup, err)
	}
	if err := RemoveKeys(ws, dup); err != nil {
		t.Fatal(err)
	}
	if LayerHas(ws, "theme") || !LayerHas(ws, "agents.max_turns") {
		t.Fatal("removed the wrong keys")
	}
}

// scopes merges as a whole table: an edit of one scope for this
// workspace must not drop the scopes you set for every workspace.
func TestSaveChangedKeepsUserScopesForWholeTables(t *testing.T) {
	dir := t.TempDir()
	user, ws := filepath.Join(dir, "user.toml"), filepath.Join(dir, "ws.toml")
	_ = os.WriteFile(user, []byte("schema = 1\n[scopes]\nnetwork = \"auto\"\n"), 0o644)
	c, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	before := Snapshot(c)
	c.Scopes["write"] = "auto"
	if _, err := SaveChanged(ws, before, c); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Scopes["network"] != "auto" || c2.Scopes["write"] != "auto" {
		t.Fatalf("scopes after reload = %v", c2.Scopes)
	}
}

func TestDuplicateKeysTreatsScopesAsOneTable(t *testing.T) {
	dir := t.TempDir()
	user, ws := filepath.Join(dir, "user.toml"), filepath.Join(dir, "ws.toml")
	_ = os.WriteFile(user, []byte("schema = 1\n[scopes]\nnetwork = \"auto\"\n"), 0o644)
	_ = os.WriteFile(ws, []byte("schema = 1\n[scopes]\nnetwork = \"auto\"\nwrite = \"auto\"\n"), 0o644)
	if dup, _ := DuplicateKeys(user, ws); len(dup) != 0 {
		t.Fatalf("partial scopes copy reported: %v", dup)
	}
}
