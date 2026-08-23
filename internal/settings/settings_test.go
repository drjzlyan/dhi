package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func TestDefaultsAreSane(t *testing.T) {
	cfg := Defaults()
	if cfg.Theme != theme.Dark().Name || cfg.Editor.TabWidth != 4 || !cfg.Editor.LineNumbers {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestPrecedenceUserOverDefaultsWorkspaceOverUser(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	ws := filepath.Join(dir, "ws.toml")

	os.WriteFile(user, []byte("theme = \"light-paper\"\n\n[editor]\ntab_width = 8\n"), 0o644)
	os.WriteFile(ws, []byte("[editor]\ntab_width = 2\n"), 0o644)

	cfg, err := Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "light-paper" {
		t.Errorf("user theme lost: %+v", cfg)
	}
	if cfg.Editor.TabWidth != 2 {
		t.Errorf("workspace tab_width did not override: %d", cfg.Editor.TabWidth)
	}
	if !cfg.Editor.LineNumbers { // unset in both layers → default survives
		t.Error("default line_numbers dropped")
	}
}

func TestBoolFalseOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	os.WriteFile(p, []byte("[editor]\nline_numbers = false\n"), 0o644)

	cfg, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Editor.LineNumbers {
		t.Error("explicit false overridden by default true")
	}
}

func TestMissingFilesAreFine(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b"))
	if err != nil || cfg.Theme != theme.Dark().Name {
		t.Errorf("cfg=%+v err=%v", cfg, err)
	}
}

func TestMalformedFileErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(p, []byte("not toml ]]"), 0o644)
	if _, err := Load(p, ""); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("err = %v", err)
	}
}

func TestUnknownKeysDetected(t *testing.T) {
	data := []byte("them = \"dark\"\n\n[editr]\ntab_width = 4\n\n[terminal]\nscrollback = 5\n")
	unknown, err := UnknownKeys(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 2 || unknown[0] != "editr.tab_width" || unknown[1] != "them" {
		t.Errorf("unknown = %v", unknown)
	}

	clean := []byte("[terminal]\nscrollback = 5\n")
	if u, _ := UnknownKeys(clean); len(u) != 0 {
		t.Errorf("false positives: %v", u)
	}
}

func TestRoundTripAndSanitize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")

	cfg := Defaults()
	cfg.Theme = "light-paper"
	cfg.Editor.TabWidth = 2
	cfg.Terminal.Scrollback = 250
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}

	back, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if back != cfg {
		t.Errorf("round trip mismatch:\n%+v\n%+v", back, cfg)
	}

	// out-of-range values sanitize back to defaults
	broken := Defaults()
	broken.Editor.TabWidth = 99
	broken.Terminal.Scrollback = 1
	broken.Theme = "neon-night"
	broken.Save(p)
	fixed, _ := Load(p, "")
	if fixed != Defaults() {
		t.Errorf("sanitize failed: %+v", fixed)
	}
}

func TestApplySwapsLiveTheme(t *testing.T) {
	before := theme.Current
	cfg := Defaults()
	cfg.Theme = theme.Light().Name
	cfg.Apply()
	if theme.Current.Name != theme.Light().Name {
		t.Fatal("Apply did not swap live theme")
	}
	theme.Current = before // restore for other tests

	// unknown theme keeps current
	cfg.Theme = "nope"
	cfg.Apply()
	if theme.Current.Name != before.Name {
		t.Error("unknown theme changed the active one")
	}
}
