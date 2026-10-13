package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	dhisettings "github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func layeredSurface(t *testing.T) (*Model, string, string) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	dir := t.TempDir()
	user, ws := filepath.Join(dir, "user", "config.toml"), filepath.Join(dir, "ws", ".dhi", "config.toml")
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte("schema = 1\n[editor]\ntab_width = 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := dhisettings.Load(user, ws)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, ws, Deps{UserConfig: user, WorkspaceConfig: ws})
	m.Resize(120, 60)
	_ = m.View() // CONFIG renders its row keys
	return m, user, ws
}

func read(t *testing.T, p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// F-090: a CONFIG edit saves only that key, for this workspace by
// default; the user's other values are never copied in.
func TestConfigEditSavesOnlyTheKeyForThisWorkspace(t *testing.T) {
	m, user, ws := layeredSurface(t)
	m.cursor = rowReducedMotion
	feed(m, "enter")
	if got := read(t, ws); !strings.Contains(got, "reduced_motion = true") || strings.Contains(got, "tab_width") {
		t.Fatalf("workspace layer:\n%s", got)
	}
	if strings.Contains(read(t, user), "reduced_motion") {
		t.Fatal("wrote the user layer")
	}
	if !strings.Contains(m.flash, "for this workspace") {
		t.Fatalf("flash = %q", m.flash)
	}
}

// s switches to every workspace: the edit lands in your config and this
// workspace's own value for it is dropped, so yours governs here too.
func TestConfigEditEverywhereWinsHere(t *testing.T) {
	m, user, ws := layeredSurface(t)
	m.cursor = rowReducedMotion
	feed(m, "enter") // this workspace: true
	feed(m, "s")
	feed(m, "enter") // every workspace: false
	if !strings.Contains(read(t, user), "reduced_motion = false") {
		t.Fatalf("user layer:\n%s", read(t, user))
	}
	if strings.Contains(read(t, ws), "reduced_motion") {
		t.Fatalf("workspace still overrides:\n%s", read(t, ws))
	}
}

// Profile settings are personal: always your config.
func TestProfileRowsSaveToYou(t *testing.T) {
	m, user, ws := layeredSurface(t)
	m.cursor = rowEmoji
	feed(m, "enter")
	if !strings.Contains(read(t, user), "emoji") || strings.Contains(read(t, ws), "emoji") {
		t.Fatalf("user:\n%s\nws:\n%s", read(t, user), read(t, ws))
	}
}
