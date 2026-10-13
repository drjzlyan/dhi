package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-070: a proposal is checked against the whole stack and applied as
// one key, leaving the rest of the layer alone.
func TestCheckAndSetLayerKey(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	ws := filepath.Join(dir, ".dhi", "config.toml")
	if err := os.MkdirAll(filepath.Dir(ws), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws, []byte("schema = 1\n[editor]\ntab_width = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := CheckChange(user, ws, ws, "budgets.task_usd", "2.5")
	if err != nil || old != "0" {
		t.Fatalf("check = %q %v", old, err)
	}
	for key, raw := range map[string]string{
		"security.sandbox": "off",  // never proposable
		"agents.max_turns": "lots", // wrong type
		"agents.autonomy":  "yolo", // fails validation on load
		"budgets.task_usd": "-1",
	} {
		if _, err := CheckChange(user, ws, ws, key, raw); err == nil {
			t.Errorf("%s=%s: want a refusal", key, raw)
		}
	}
	if err := SetLayerKey(ws, "budgets.task_usd", "2.5"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(user, ws)
	if err != nil || c.Budgets.TaskUSD != 2.5 || c.Editor.TabWidth != 2 {
		t.Fatalf("after apply: %+v %v", c, err)
	}
	b, _ := os.ReadFile(ws)
	if strings.Contains(string(b), "theme") || strings.Contains(string(b), "scrollback") {
		t.Fatalf("apply must touch one key, not freeze defaults into the layer:\n%s", b)
	}
	if v := Value(c, "budgets.task_usd"); v != "2.5" {
		t.Fatalf("Value = %q", v)
	}
}

func TestSaveProfileKeepsOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("schema = 1\ntheme = \"light-paper\"\n"), 0o644)
	p := Profile{Role: "founder", Writing: ProfileWriting{Tone: "casual", Emoji: true}, Reading: ProfileReading{Format: "bullets"}}
	if err := SaveProfile(path, p); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "")
	if err != nil || c.Theme != "light-paper" || c.Profile.Role != "founder" || !c.Profile.Writing.Emoji || c.Profile.Reading.Format != "bullets" {
		t.Fatalf("loaded %+v / %v", c.Profile, err)
	}
	if err := SaveProfile(path, Profile{Writing: ProfileWriting{Tone: "snarky"}}); err == nil {
		t.Fatal("invalid profile saved")
	}
	if c, _ := Load(path, ""); c.Profile.Role != "founder" {
		t.Fatal("refused save changed the file")
	}
}
