package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func newSurface(t *testing.T) (*Model, string) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	path := filepath.Join(t.TempDir(), ".dhi", "config.toml")
	m := New(settings.Defaults(), path)
	m.Resize(80, 24)
	return m, path
}

func TestNavigateAndCycleTheme(t *testing.T) {
	m, _ := newSurface(t)
	if !strings.Contains(ansi.Strip(m.View()), "dark-futuristic") {
		t.Fatal("theme row missing initial value")
	}

	feed(m, "enter") // cycle theme → light
	if m.cfg.Theme != theme.Light().Name {
		t.Fatalf("theme = %q", m.cfg.Theme)
	}
	if theme.Current.Name != theme.Light().Name {
		t.Fatal("live theme did not switch")
	}
	if !strings.Contains(ansi.Strip(m.View()), "light-paper") {
		t.Error("view not updated")
	}

	feed(m, "h") // cycle back
	if m.cfg.Theme != theme.Dark().Name || theme.Current.Name != theme.Dark().Name {
		t.Error("cycle back failed")
	}
}

func TestTabWidthCyclesAndPersists(t *testing.T) {
	m, path := newSurface(t)
	feed(m, "j") // tab_width
	feed(m, "l") // 4 → 8

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tab_width = 8") {
		t.Errorf("persisted file:\n%s", data)
	}
	back, err := settings.Load(path, "")
	if err != nil || back.Editor.TabWidth != 8 {
		t.Errorf("reload = %+v err=%v", back, err)
	}
}

func TestLineNumbersToggle(t *testing.T) {
	m, _ := newSurface(t)
	feed(m, "j", "j") // line_numbers
	was := m.cfg.Editor.LineNumbers
	feed(m, "enter")
	if m.cfg.Editor.LineNumbers == was {
		t.Fatal("toggle no-op")
	}
}

func TestScrollbackBounds(t *testing.T) {
	m, _ := newSurface(t)
	for i := 0; i < 20; i++ {
		feed(m, "j", "j", "j")
		feed(m, "h") // decrease repeatedly
	}
	if got := m.cfg.Terminal.Scrollback; got < 100 {
		t.Errorf("scrollback below floor: %d", got)
	}
}

func TestNoSavePathIsSessionOnly(t *testing.T) {
	m := New(settings.Defaults(), "")
	m.Resize(60, 20)
	feed(m, "enter")
	if !strings.Contains(ansi.Strip(m.View()), "session-only") {
		t.Errorf("flash missing:\n%s", ansi.Strip(m.View()))
	}
}

func feed(m *Model, keys ...string) {
	for _, k := range keys {
		m.HandleKey(k)
	}
}
