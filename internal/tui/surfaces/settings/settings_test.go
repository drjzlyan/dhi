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
	m.cursor = rowTheme
	if !strings.Contains(ansi.Strip(fullView(m)), "dark-futuristic") {
		t.Fatal("theme row missing initial value")
	}

	feed(m, "enter") // cycle theme → light
	if m.cfg.Theme != theme.Light().Name {
		t.Fatalf("theme = %q", m.cfg.Theme)
	}
	if theme.Current.Name != theme.Light().Name {
		t.Fatal("live theme did not switch")
	}
	if !strings.Contains(ansi.Strip(fullView(m)), "light-paper") {
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
	m.cursor = rowTheme
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
	m.cursor = rowTheme
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
	m.cursor = rowTheme
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
	m.cursor = rowTheme
	feed(m, "enter")
	if !strings.Contains(ansi.Strip(fullView(m)), "session-only") {
		t.Errorf("flash missing:\n%s", ansi.Strip(fullView(m)))
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
	if !strings.Contains(ansi.Strip(fullView(m)), "may write files") {
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
	feed(m, "enter") // nothing to switch on yet: asks for the co-author
	if m.cfg.Conventions.Commit.CoAuthorEnabled || m.dkind != dlgConfigValue {
		t.Fatalf("co-author enabled without a value, or no editor: dkind=%v", m.dkind)
	}
	for _, r := range "Bot <bot@x.io>" {
		m.HandleKey(string(r))
	}
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
	if m.cfg.Conventions.Copyright.Enabled || m.dkind != dlgConfigValue || !strings.Contains(ansi.Strip(fullView(m)), "holder") {
		t.Fatalf("enabled=%v; want the holder editor:\n%s", m.cfg.Conventions.Copyright.Enabled, ansi.Strip(fullView(m)))
	}
	for _, r := range "Acme Inc" {
		m.HandleKey(string(r))
	}
	feed(m, "enter")
	if c := m.cfg.Conventions.Copyright; !c.Enabled || c.Holder != "Acme Inc" {
		t.Fatalf("copyright %+v", c)
	}
}

func TestTypedValuesAreCheckedAndSaved(t *testing.T) {
	m, path := newSurface(t)
	m.cursor = rowMaxTurns
	feed(m, "e")
	if m.dkind != dlgConfigValue || !strings.Contains(ansi.Strip(fullView(m)), "0 = the CLI's default") {
		t.Fatalf("editor:\n%s", ansi.Strip(fullView(m)))
	}
	feed(m, "ctrl+a", "delete", "4", "2", "enter")
	if m.cfg.Agents.MaxTurns != 42 || m.dlg != nil {
		t.Fatalf("max turns %d dlg %v", m.cfg.Agents.MaxTurns, m.dlg != nil)
	}
	m.cursor = rowMaxIter
	feed(m, "e", "ctrl+a", "delete", "9", "9", "enter")
	if m.dform == nil || !strings.Contains(m.dform.Err, "between 1 and 10") || m.cfg.Agents.MaxIterations != 3 {
		t.Fatalf("out of range accepted: %+v %d", m.dform, m.cfg.Agents.MaxIterations)
	}
	feed(m, "esc")
	m.cursor = rowTaskUSD
	feed(m, "e", "ctrl+a", "delete", "$", "3", ".", "5", "enter")
	m.cursor = rowSandbox
	feed(m, "enter")
	m.cursor = rowReflectModel
	feed(m, "enter")
	for _, r := range "claude-haiku-4-5" {
		m.HandleKey(string(r))
	}
	feed(m, "enter")
	c, err := settings.Load(path, "")
	if err != nil || c.Agents.MaxTurns != 42 || c.Budgets.TaskUSD != 3.5 || c.Security.Sandbox != settings.SandboxOff ||
		c.Agents.ReflectModel != "claude-haiku-4-5" {
		t.Fatalf("persisted %+v %+v %v", c.Agents, c.Budgets, err)
	}
}

func TestKeysAndLanguagesAreEditable(t *testing.T) {
	m, path := newSurface(t)
	m.cursor = rowKeys
	feed(m, "enter")
	for _, r := range "ctrl+k=ctrl+p, x=d" {
		m.HandleKey(string(r))
	}
	feed(m, "enter")
	if m.cfg.Keys["ctrl+k"] != "ctrl+p" || m.cfg.Keys["x"] != "d" {
		t.Fatalf("keys %v", m.cfg.Keys)
	}
	feed(m, "enter", "ctrl+e")
	for _, r := range ", ctrl+c=q" {
		m.HandleKey(string(r))
	}
	feed(m, "enter")
	if m.dform == nil || !strings.Contains(m.dform.Err, "ctrl+c always quits") {
		t.Fatalf("ctrl+c remap accepted: %+v", m.dform)
	}
	feed(m, "esc")
	m.cursor = rowLanguages
	feed(m, "enter", "right", "enter") // first language → off
	c, err := settings.Load(path, "")
	if err != nil || c.Keys["x"] != "d" {
		t.Fatalf("keys not persisted: %v %v", c.Keys, err)
	}
	if lc := c.Editor.Languages["go"]; lc.Enabled == nil || *lc.Enabled {
		t.Fatalf("go not switched off: %+v", c.Editor.Languages)
	}
	if v := ansi.Strip(fullView(m)); !strings.Contains(v, "of") {
		t.Fatalf("languages row:\n%s", v)
	}
}

func TestWorktreeRootRowCyclesPersistsAndSaysItNeedsARestart(t *testing.T) {
	m, path := newSurface(t)
	m.cursor = rowWorktrees
	if got := m.cfg.Worktrees.Root; got != "" {
		t.Fatalf("default root = %q", got)
	}
	if !strings.Contains(ansi.Strip(fullView(m)), "task worktrees folder") {
		t.Error("worktrees row not rendered")
	}
	feed(m, "l") // inside .dhi → next to the workspace
	if m.cfg.Worktrees.Root != "../.dhi-worktrees" {
		t.Fatalf("root = %q", m.cfg.Worktrees.Root)
	}
	if !strings.Contains(m.flash, "restart") || !strings.Contains(m.flash, "existing ones stay") {
		t.Fatalf("flash = %q", m.flash)
	}
	back, err := settings.Load(path, "")
	if err != nil {
		t.Fatalf("saved config does not reload: %v", err)
	}
	if back.Worktrees.Root != "../.dhi-worktrees" {
		t.Fatalf("persisted root = %q", back.Worktrees.Root)
	}
	// It is personal: the tracked conventions file is not touched by it.
	if data, _ := os.ReadFile(settings.ConventionsPath(path)); strings.Contains(string(data), "worktrees") {
		t.Fatalf("worktrees leaked into the tracked conventions: %s", data)
	}
	feed(m, "l", "l") // → ~/dhi-worktrees → wraps to default
	if m.cfg.Worktrees.Root != "" {
		t.Fatalf("cycle did not wrap to the default: %q", m.cfg.Worktrees.Root)
	}
}

// F-066: the agents & budgets rows cycle presets, persist, and say the
// change is live from the next spawn.
func TestAgentsAndBudgetRowsPersist(t *testing.T) {
	m, path := newSurface(t)
	m.cursor = rowTaskUSD
	view := ansi.Strip(fullView(m))
	for _, want := range []string{"AGENTS & BUDGETS", "budget per task", "no limit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	feed(m, "l") // no limit → $0.5
	m.cursor = rowMaxTurns
	feed(m, "l", "l") // CLI default → 10 → 20
	m.cursor = rowResume
	feed(m, "enter") // on → off
	m.cursor = rowTaskTokens
	feed(m, "h") // no limit → 2M (wraps)
	if !strings.Contains(m.flash, "next spawn") {
		t.Fatalf("flash = %q", m.flash)
	}
	if !strings.Contains(ansi.Strip(fullView(m)), "2M") {
		t.Fatal("token budget not rendered compactly")
	}
	back, err := settings.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if back.Budgets.TaskUSD != 0.5 || back.Agents.MaxTurns != 20 || back.Agents.Resume || back.Budgets.TaskTokens != 2_000_000 {
		t.Fatalf("persisted agents=%+v budgets=%+v", back.Agents, back.Budgets)
	}
	// The conventions file is untouched by an agents row.
	if _, err := os.Stat(settings.ConventionsPath(path)); err == nil {
		t.Fatal("an agents row must not write the conventions file")
	}
}

// F-068: agents.reflect toggles and persists.
func TestReflectRowPersists(t *testing.T) {
	m, path := newSurface(t)
	m.cursor = rowReflect
	if !strings.Contains(ansi.Strip(fullView(m)), "learn from turns") {
		t.Fatal("reflect row not rendered")
	}
	feed(m, "enter")
	back, err := settings.Load(path, "")
	if err != nil || back.Agents.Reflect {
		t.Fatalf("reflect not persisted off: %+v %v", back.Agents, err)
	}
}

func TestYouRowsCycleAndPersist(t *testing.T) {
	m, path := newSurface(t)
	if v := ansi.Strip(fullView(m)); !strings.Contains(v, "YOU") || !strings.Contains(v, "you are a") {
		t.Fatalf("you group missing:\n%s", v)
	}
	m.cursor = rowRole
	feed(m, "l") // role developer → tech lead
	m.cursor = rowFormat
	feed(m, "l") // prose → bullets
	m.cursor = rowEmoji
	feed(m, "enter")
	c, err := settings.Load(path, "")
	if err != nil || c.Profile.Role != "tech lead" || c.Profile.Reading.Format != "bullets" || !c.Profile.Writing.Emoji {
		t.Fatalf("persisted %+v %v", c.Profile, err)
	}
	if !strings.Contains(m.flash, "live") {
		t.Fatalf("flash %q", m.flash)
	}
}

// F-084: every setting is reachable from Settings, so nobody needs a
// config file. A new key in settings.Known() fails here until it gets a
// CONFIG row or is named as owned by another section.
func TestEverySettingHasAPlaceInSettings(t *testing.T) {
	m, _ := newSurface(t)
	m.Resize(140, 400)
	m.configView(130, 0)
	rendered := map[string]bool{}
	for _, k := range m.cfgKeys {
		rendered[k] = true
	}
	elsewhere := map[string]string{
		"schema": "internal",
		// Naming templates (CONVENTIONS section: use, edit, learn).
		"conventions.branch.review": "CONVENTIONS", "conventions.commit.ticket_pattern": "CONVENTIONS",
		"conventions.commit.subject": "CONVENTIONS", "conventions.commit.body": "CONVENTIONS",
		"conventions.pr.title": "CONVENTIONS", "conventions.pr.body": "CONVENTIONS",
		"conventions.worktree.task": "CONVENTIONS", "conventions.active": "CONVENTIONS",
		"conventions.commit.co_author_enabled": "the co-author row", "conventions.copyright.enabled": "the copyright row",
		"conventions.copyright.holder": "the copyright row",
		"conventions.copyright.year":   "the current year, always",
	}
	known := settings.Known()
	isContainer := func(k string) bool {
		for _, o := range known {
			if strings.HasPrefix(o, k+".") {
				return true
			}
		}
		return false
	}
	for _, k := range known {
		if isContainer(k) || elsewhere[k] != "" {
			continue
		}
		if !rendered[k] {
			t.Errorf("setting %s has no CONFIG row and no owner", k)
		}
	}
}

// fullView is what the user sees of this surface: the pane plus the
// flash and keymap the shell renders on its status row (ADR-0040).
func fullView(m *Model) string {
	flash, hints := m.Hints()
	return m.View() + "\n" + flash + "\n" + strings.Join(hints, " • ")
}
