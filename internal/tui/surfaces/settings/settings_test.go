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
	m := New(settings.Defaults(), path, Deps{})
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

func TestReducedMotionTogglesLiveAndPersists(t *testing.T) {
	theme.MotionForTest(t, true)
	m, path := newSurface(t)
	feed(m, "j") // reduced_motion row
	feed(m, "enter")
	if !m.cfg.ReducedMotion {
		t.Fatal("toggle did not enable reduced motion")
	}
	if theme.Motion {
		t.Fatal("live theme.Motion not disabled")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "reduced_motion = true") {
		t.Errorf("persisted file:\n%s", data)
	}
	back, err := settings.Load(path, "")
	if err != nil || !back.ReducedMotion {
		t.Errorf("reload = %+v err=%v", back, err)
	}
	feed(m, "enter") // back off
	if m.cfg.ReducedMotion || !theme.Motion {
		t.Fatal("toggle off did not restore motion")
	}
}

func TestTabWidthCyclesAndPersists(t *testing.T) {
	m, path := newSurface(t)
	feed(m, "j", "j") // tab_width
	feed(m, "l")      // 4 → 8

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
	feed(m, "j", "j", "j") // line_numbers
	was := m.cfg.Editor.LineNumbers
	feed(m, "enter")
	if m.cfg.Editor.LineNumbers == was {
		t.Fatal("toggle no-op")
	}
}

func TestScrollbackBounds(t *testing.T) {
	m, _ := newSurface(t)
	for i := 0; i < 20; i++ {
		feed(m, "j", "j", "j", "j")
		feed(m, "h") // decrease repeatedly
	}
	if got := m.cfg.Terminal.Scrollback; got < 100 {
		t.Errorf("scrollback below floor: %d", got)
	}
}

func TestNoSavePathIsSessionOnly(t *testing.T) {
	m := New(settings.Defaults(), "", Deps{})
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

func TestScopesEditorCyclesAndPersists(t *testing.T) {
	m, path := newSurface(t)
	m.cursor = rowScopesBase + 1 // scopes.write
	m.cycle(1)
	if m.cfg.Scopes["write"] != "auto" {
		t.Fatalf("write = %q, want auto", m.cfg.Scopes["write"])
	}
	m.cycle(1)
	if m.cfg.Scopes["write"] != "ask" {
		t.Fatalf("write = %q, want ask", m.cfg.Scopes["write"])
	}
	m.cycle(1)
	if m.cfg.Scopes["write"] != "deny" {
		t.Fatalf("write = %q, want deny", m.cfg.Scopes["write"])
	}
	m.cycle(1) // wraps to auto
	if m.cfg.Scopes["write"] != "auto" {
		t.Fatalf("write = %q, want wrap to auto", m.cfg.Scopes["write"])
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `write = "auto"`) {
		t.Fatalf("config not persisted: %v\n%s", err, data)
	}
	if !strings.Contains(ansi.Strip(m.View()), "scopes.write") {
		t.Error("scope row not rendered")
	}
}

func TestConventionRowsCycleAndPersist(t *testing.T) {
	m, path := newSurface(t)
	m.cursor = rowCommitFormat
	feed(m, "l") // free → conventional
	m.cursor = rowBranchTask
	feed(m, "l") // task/{slug} → feature/{slug}
	m.cursor = rowCommitCoAuthor
	feed(m, "enter")
	if m.cfg.Conventions.Commit.CoAuthorEnabled || !strings.Contains(m.flash, "co_author") {
		t.Fatalf("co-author enabled without a value: flash=%q", m.flash)
	}
	m.cfg.Conventions.Commit.CoAuthor = "Bot <bot@x.io>"
	feed(m, "enter")

	if !strings.Contains(m.flash, "agents use it from their next action") {
		t.Errorf("flash = %q", m.flash)
	}
	back, err := settings.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	c := back.Conventions
	if c.Commit.Format != "conventional" || c.Branch.Task != "feature/{slug}" || !c.Commit.CoAuthorEnabled {
		t.Fatalf("persisted conventions = %+v", c)
	}
}

func TestCopyrightRowNeedsHolder(t *testing.T) {
	m, _ := newSurface(t)
	m.cursor = rowCopyright
	feed(m, "enter")
	if m.cfg.Conventions.Copyright.Enabled || !strings.Contains(m.flash, "holder") {
		t.Fatalf("enabled=%v flash=%q; want refusal naming the holder key",
			m.cfg.Conventions.Copyright.Enabled, m.flash)
	}
}
