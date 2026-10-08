package wizard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/starter"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/conventions"
	"github.com/drjzlyan/dhi/internal/gitcore"
	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

// run executes a command and feeds every message it yields back into the
// wizard, flattening batches. Reduced motion keeps tea.Tick out of it.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	default:
		if _, quit := msg.(tea.QuitMsg); quit {
			return
		}
		run(m, m.Update(msg))
	}
}

func key(m *Model, keys ...string) {
	for _, k := range keys {
		m.HandleKey(k)
		run(m, m.TakeCmd())
	}
}

func typeText(m *Model, s string) {
	for _, r := range s {
		key(m, string(r))
	}
}

type fixture struct {
	env       *Env
	root      string
	persisted []setup.State
	setCalls  []gitcore.Identity
	saveRoot  string
	savedConv *conventions.Config
}

func newFixture(t *testing.T, withWorkspace bool) *fixture {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	theme.MotionForTest(t, false)
	root := t.TempDir()
	f := &fixture{root: root}
	f.env = &Env{
		Version: "9.9.9", CWD: root,
		Discover:      setup.DiscoverMembers,
		InitWorkspace: setup.InitWorkspace,
		Identity: func(context.Context) (gitcore.Identity, error) {
			return gitcore.Identity{Name: "Ada", Email: "ada@example.com"}, nil
		},
		SetIdentity: func(_ context.Context, id gitcore.Identity) error {
			f.setCalls = append(f.setCalls, id)
			return nil
		},
		Conventions: conventions.Defaults(),
		SaveConventions: func(r string, c conventions.Config) error {
			f.saveRoot, f.savedConv = r, &c
			p := filepath.Join(r, ".dhi", settings.ConventionsFile)
			return settings.SaveConventions(p, c)
		},
		Persist: func(_ string, s setup.State) error { f.persisted = append(f.persisted, s); return nil },
		Now:     func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
	if withWorkspace {
		if _, err := setup.InitWorkspace(root, nil); err != nil {
			t.Fatal(err)
		}
		f.env.Root = root
	}
	return f
}

func (f *fixture) wizard() *Model {
	m := New(f.env, setup.State{}, DefaultSteps(f.env, "9.9.9")...)
	m.Resize(100, 40)
	run(m, m.Init())
	return m
}

func plain(m *Model) string { return ansi.Strip(m.View()) }

func TestFullFlowCreatesWorkspaceAndRelaunches(t *testing.T) {
	f := newFixture(t, false)
	if err := os.MkdirAll(filepath.Join(f.root, "web", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := f.wizard()

	key(m, "enter") // welcome → workspace
	if !strings.Contains(plain(m), "web") || !strings.Contains(plain(m), "No DHI workspace") {
		t.Fatalf("workspace step missing:\n%s", plain(m))
	}
	key(m, "enter") // create
	if f.env.Root != f.root || !f.env.Changed {
		t.Fatalf("workspace not created: root=%q changed=%v", f.env.Root, f.env.Changed)
	}
	if _, err := os.Stat(filepath.Join(f.root, ".dhi", "workspace.toml")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain(m), "git identity found") {
		t.Fatalf("expected identity found:\n%s", plain(m))
	}
	key(m, "enter")          // identity → conventions
	key(m, "right", "enter") // commits: free → conventional, submit
	if f.savedConv == nil || f.savedConv.Commit.Format != conventions.FormatConventional || f.saveRoot != f.root {
		t.Fatalf("conventions not saved at the workspace: root=%q conv=%+v", f.saveRoot, f.savedConv)
	}
	if !strings.Contains(plain(m), "You're set up") {
		t.Fatalf("done step expected:\n%s", plain(m))
	}
	key(m, "enter")
	if !m.NeedsRelaunch() || m.Finished() {
		t.Fatalf("relaunch=%v finished=%v; a changed wizard must relaunch, not release", m.NeedsRelaunch(), m.Finished())
	}
	cfg, err := settings.Load("", filepath.Join(f.root, ".dhi", "config.toml"))
	if err != nil || cfg.Conventions.Commit.Format != conventions.FormatConventional {
		t.Fatalf("settings.Load did not accept the written file: %v %+v", err, cfg.Conventions.Commit)
	}
	last := f.persisted[len(f.persisted)-1]
	if !last.Finished || !last.Has("workspace") || !last.Has("conventions") {
		t.Fatalf("progress not persisted: %+v", last)
	}
}

func TestNothingChangedReleasesTheShell(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	if strings.Contains(strings.Join(titles(m), ","), "workspace") {
		t.Fatalf("workspace step shown for an existing workspace: %v", titles(m))
	}
	key(m, "enter", "enter", "enter", "enter") // welcome, identity, conventions (unchanged), done
	if !m.Finished() || m.NeedsRelaunch() {
		t.Fatalf("finished=%v relaunch=%v; an unchanged run must just release the shell", m.Finished(), m.NeedsRelaunch())
	}
}

func titles(m *Model) []string {
	var out []string
	for _, it := range m.stepItems() {
		out = append(out, it.Label)
	}
	return out
}

func TestIdentityIsWrittenOnlyAfterConfirmation(t *testing.T) {
	f := newFixture(t, true)
	f.env.Identity = func(context.Context) (gitcore.Identity, error) { return gitcore.Identity{}, gitcore.ErrIdentityUnset }
	m := f.wizard()
	key(m, "enter") // → identity (unset → form)
	if !strings.Contains(plain(m), "Who are you?") {
		t.Fatalf("form expected:\n%s", plain(m))
	}
	typeText(m, "Grace Hopper")
	key(m, "tab")
	typeText(m, "grace@navy.mil")
	key(m, "enter") // → confirm
	if len(f.setCalls) != 0 {
		t.Fatal("identity written before confirmation")
	}
	if !strings.Contains(plain(m), "git config --global user.name 'Grace Hopper'") {
		t.Fatalf("exact command not shown:\n%s", plain(m))
	}
	key(m, "esc") // back to editing, still nothing written
	if len(f.setCalls) != 0 || !strings.Contains(plain(m), "Who are you?") {
		t.Fatalf("esc must return to the form without writing; calls=%v", f.setCalls)
	}
	key(m, "enter", "enter") // submit, confirm
	if len(f.setCalls) != 1 || f.setCalls[0] != (gitcore.Identity{Name: "Grace Hopper", Email: "grace@navy.mil"}) {
		t.Fatalf("calls = %+v", f.setCalls)
	}
	if !strings.Contains(strings.Join(f.env.Applied, "|"), "git identity set") {
		t.Fatalf("applied = %v", f.env.Applied)
	}
}

func TestIdentityRejectsBadEmailAndStaysInForm(t *testing.T) {
	f := newFixture(t, true)
	f.env.Identity = func(context.Context) (gitcore.Identity, error) { return gitcore.Identity{}, gitcore.ErrIdentityUnset }
	m := f.wizard()
	key(m, "enter")
	typeText(m, "Ada")
	key(m, "tab")
	typeText(m, "not-an-email")
	key(m, "enter")
	if !strings.Contains(plain(m), "email must look like") || len(f.setCalls) != 0 {
		t.Fatalf("expected validation error in the form:\n%s", plain(m))
	}
}

func TestIdentityWriteFailureKeepsTheStep(t *testing.T) {
	f := newFixture(t, true)
	f.env.Identity = func(context.Context) (gitcore.Identity, error) { return gitcore.Identity{}, gitcore.ErrIdentityUnset }
	f.env.SetIdentity = func(context.Context, gitcore.Identity) error { return errors.New("disk is read-only") }
	m := f.wizard()
	key(m, "enter")
	typeText(m, "Ada")
	key(m, "tab")
	typeText(m, "ada@x.io")
	key(m, "enter", "enter")
	if m.steps[m.idx].ID() != "identity" || !strings.Contains(plain(m), "disk is read-only") {
		t.Fatalf("failure must keep the step and show the error:\n%s", plain(m))
	}
}

func TestIdentityUnavailableShowsManualCommands(t *testing.T) {
	f := newFixture(t, true)
	f.env.Identity, f.env.SetIdentity = nil, nil
	m := f.wizard()
	key(m, "enter")
	if !strings.Contains(plain(m), "git config --global user.email") || !strings.Contains(plain(m), "isn't installed") {
		t.Fatalf("manual fix expected:\n%s", plain(m))
	}
	key(m, "enter") // continue anyway
	if m.steps[m.idx].ID() != "conventions" {
		t.Fatalf("step = %s", m.steps[m.idx].ID())
	}
}

func TestWorkspaceCreationFailureKeepsTheStep(t *testing.T) {
	f := newFixture(t, false)
	f.env.InitWorkspace = func(string, []setup.MemberSpec) ([]setup.MemberSpec, error) {
		return nil, errors.New("permission denied")
	}
	m := f.wizard()
	key(m, "enter", "enter")
	if m.steps[m.idx].ID() != "workspace" || !strings.Contains(plain(m), "permission denied") {
		t.Fatalf("failure must keep the step:\n%s", plain(m))
	}
	if f.env.Root != "" || f.env.Changed {
		t.Fatal("a failed create must not mark the env changed")
	}
}

func TestSkipStepAndBack(t *testing.T) {
	f := newFixture(t, false)
	m := f.wizard()
	key(m, "enter") // → workspace
	key(m, "esc")   // skip it → identity
	if m.steps[m.idx].ID() != "identity" || f.env.Root != "" {
		t.Fatalf("step=%s root=%q", m.steps[m.idx].ID(), f.env.Root)
	}
	if !m.skipped["workspace"] {
		t.Fatal("skipped step not recorded")
	}
	key(m, "ctrl+b")
	if m.steps[m.idx].ID() != "workspace" || m.skipped["workspace"] {
		t.Fatalf("back should reopen the skipped step; step=%s", m.steps[m.idx].ID())
	}
}

func TestConventionsWithoutWorkspaceSaveToUserScope(t *testing.T) {
	f := newFixture(t, false)
	var gotRoot = "unset"
	f.env.SaveConventions = func(r string, c conventions.Config) error { gotRoot = r; return nil }
	m := f.wizard()
	key(m, "enter", "esc", "enter") // welcome, skip workspace, identity
	key(m, "right", "enter")        // change commits
	if gotRoot != "" {
		t.Fatalf("root = %q, want user scope (\"\")", gotRoot)
	}
}

func TestConventionsRejectInvalidAndStay(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	key(m, "enter", "enter") // → conventions
	key(m, "tab", "tab", "tab", "tab")
	typeText(m, "MIT")    // license set, but...
	key(m, "shift+tab")   // → copyright holder
	key(m, "shift+tab")   // → co-author
	typeText(m, "broken") // invalid co-author (no <email>)
	key(m, "enter")
	if m.steps[m.idx].ID() != "conventions" || !strings.Contains(plain(m), "co_author") {
		t.Fatalf("invalid conventions must stay with the reason:\n%s", plain(m))
	}
	if f.savedConv != nil {
		t.Fatal("invalid conventions were saved")
	}
}

func TestCtrlXSkipsEverythingButStillRelaunchesIfChanged(t *testing.T) {
	f := newFixture(t, false)
	m := f.wizard()
	key(m, "enter", "enter") // welcome, create workspace
	key(m, "ctrl+x")
	if !m.NeedsRelaunch() {
		t.Fatal("a workspace was created; skipping the rest must still relaunch")
	}

	g := newFixture(t, true)
	m2 := g.wizard()
	key(m2, "ctrl+x")
	if !m2.Finished() || m2.NeedsRelaunch() {
		t.Fatal("skipping with no changes must release the shell")
	}
}

func TestReducedMotionShowsWelcomeInFull(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	if !strings.Contains(plain(m), "change it later from Settings") {
		t.Fatalf("reduced motion must not hide text behind a clock:\n%s", plain(m))
	}
	theme.MotionForTest(t, true)
	m2 := New(f.env, setup.State{}, DefaultSteps(f.env, "9.9.9")...)
	m2.Resize(100, 40)
	if strings.Contains(plain(m2), "Everything has") || strings.Contains(plain(m2), "Welcome.") {
		t.Fatalf("with motion the text starts hidden:\n%s", plain(m2))
	}
	for i := 0; i < 60; i++ {
		m2.Update(tickMsg{})
	}
	if !strings.Contains(plain(m2), "change it later from Settings") {
		t.Fatalf("text never finished revealing:\n%s", plain(m2))
	}
}

func TestNoClockUnderReducedMotion(t *testing.T) {
	f := newFixture(t, true)
	m := New(f.env, setup.State{}, DefaultSteps(f.env, "9.9.9")...)
	if m.tick() != nil {
		t.Fatal("reduced motion must arm no clock")
	}
}

func TestGoldenSteps(t *testing.T) {
	f := newFixture(t, false)
	os.MkdirAll(filepath.Join(f.root, "api", ".git"), 0o755)
	os.MkdirAll(filepath.Join(f.root, "web", ".git"), 0o755)
	f.env.CWD = "/work/acme"
	m := f.wizard()
	golden.Snapshot(t, "wizard_welcome", plain(m))
	key(m, "enter")
	golden.Snapshot(t, "wizard_workspace", plain(m))
	key(m, "esc")
	golden.Snapshot(t, "wizard_identity_found", plain(m))
	key(m, "enter")
	golden.Snapshot(t, "wizard_conventions", plain(m))
	key(m, "enter")
	golden.Snapshot(t, "wizard_done", plain(m))
}

func TestIdentityGitFailureIsNotTreatedAsUnset(t *testing.T) {
	f := newFixture(t, true)
	f.env.Identity = func(context.Context) (gitcore.Identity, error) {
		return gitcore.Identity{}, errors.New("fork/exec /nope/git: no such file or directory")
	}
	m := f.wizard()
	key(m, "enter")
	out := plain(m)
	if strings.Contains(out, "Who are you?") || !strings.Contains(out, "no such file") {
		t.Fatalf("a broken git must not prompt for a name:\n%s", out)
	}
}

func TestCompletedWorkspaceStepStaysInStepper(t *testing.T) {
	f := newFixture(t, false)
	m := f.wizard()
	key(m, "enter", "enter") // welcome, create the workspace
	if !strings.Contains(plain(m), "✓ workspace") {
		t.Fatalf("created workspace step vanished from the stepper:\n%s", plain(m))
	}
}

// A re-run over an existing workspace must not advertise a workspace step
// that will never be shown, even though earlier progress recorded it.
func TestRerunHidesInapplicableStepsAheadOfYou(t *testing.T) {
	f := newFixture(t, true)
	st := setup.State{Finished: true}
	st.Mark("workspace", time.Now())
	m := New(f.env, st, DefaultSteps(f.env, "9.9.9")...)
	if strings.Contains(strings.Join(titles(m), ","), "workspace") {
		t.Fatalf("stepper = %v", titles(m))
	}
}

// ---- team step (F-045) ----

type teamFixture struct {
	*fixture
	applied []string
	engines []string
	roster  int
}

func newTeamFixture(t *testing.T) *teamFixture {
	t.Helper()
	f := &teamFixture{fixture: newFixture(t, true)}
	f.env.RosterCount = func() int { return f.roster }
	f.env.DetectCLIs = func() map[string]string {
		return map[string]string{"claude": "2.1.0", "codex": "", "opencode": "0.9"}
	}
	f.env.ApplyTeam = func(slug string) (starter.Result, error) {
		f.applied = append(f.applied, slug)
		return starter.Result{Team: slug, Created: []string{"atlas", "forge"}, TeamCreated: true}, nil
	}
	f.env.SetEngine = func(e string) error { f.engines = append(f.engines, e); return nil }
	return f
}

// toTeam walks welcome → identity → conventions to land on the team step.
func (f *teamFixture) toTeam(t *testing.T) *Model {
	t.Helper()
	m := f.wizard()
	key(m, "enter", "enter", "enter")
	if m.steps[m.idx].ID() != "team" {
		t.Fatalf("on step %s, want team:\n%s", m.steps[m.idx].ID(), plain(m))
	}
	return m
}

func TestTeamStepPreviewsTheTeamAndItsLead(t *testing.T) {
	f := newTeamFixture(t)
	m := f.toTeam(t)
	v := plain(m)
	for _, want := range []string{"Squad", "Atlas", "planner", "mentor", "★ lead", "Forge", "coding CLIs found: claude, opencode"} {
		if !strings.Contains(v, want) {
			t.Errorf("team step lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "codex") {
		t.Errorf("an uninstalled CLI must not be offered as an engine:\n%s", v)
	}
	key(m, "right") // squad → studio
	if !strings.Contains(plain(m), "Anvil") {
		t.Fatalf("preview did not follow the template toggle:\n%s", plain(m))
	}
	key(m, "left", "left") // → solo
	if v := plain(m); !strings.Contains(v, "you lead this team") || strings.Contains(v, "★ lead") {
		t.Fatalf("solo is led by the human:\n%s", v)
	}
}

func TestTeamStepAppliesTeamAndEngineAndRelaunches(t *testing.T) {
	f := newTeamFixture(t)
	m := f.toTeam(t)
	key(m, "enter")
	if strings.Join(f.applied, ",") != "squad" || strings.Join(f.engines, ",") != "cli:claude" {
		t.Fatalf("applied=%v engines=%v", f.applied, f.engines)
	}
	if !f.env.Changed || !strings.Contains(strings.Join(f.env.Applied, "|"), "team squad created: atlas, forge") ||
		!strings.Contains(strings.Join(f.env.Applied, "|"), "default engine cli:claude") {
		t.Fatalf("env = changed:%v applied:%v", f.env.Changed, f.env.Applied)
	}
	key(m, "enter") // done → relaunch
	if !m.NeedsRelaunch() {
		t.Fatal("creating a team must relaunch so the runtime sees the roster")
	}
}

func TestTeamStepDecideLaterLeavesEngineAlone(t *testing.T) {
	f := newTeamFixture(t)
	m := f.toTeam(t)
	key(m, "tab", "left") // engine: claude → decide later (wraps)
	key(m, "enter")
	if len(f.applied) != 1 || len(f.engines) != 0 {
		t.Fatalf("applied=%v engines=%v; 'decide later' must not set an engine", f.applied, f.engines)
	}
}

func TestTeamStepHiddenWhenAgentsExistOrNoWorkspace(t *testing.T) {
	f := newTeamFixture(t)
	f.roster = 3
	if strings.Contains(strings.Join(titles(f.wizard()), ","), "team") {
		t.Fatal("team step shown for a workspace that already has employees")
	}
	g := newFixture(t, false)
	g.env.ApplyTeam = func(string) (starter.Result, error) { return starter.Result{}, nil }
	if strings.Contains(strings.Join(titles(g.wizard()), ","), "team") {
		t.Fatal("team step shown with no workspace to put a team in")
	}
}

func TestTeamStepApplyFailureKeepsTheStepWithTheError(t *testing.T) {
	f := newTeamFixture(t)
	f.env.ApplyTeam = func(string) (starter.Result, error) {
		return starter.Result{}, errors.New("agent \"sage\" is archived")
	}
	m := f.toTeam(t)
	key(m, "enter")
	if m.steps[m.idx].ID() != "team" || !strings.Contains(plain(m), "archived") || f.env.Changed {
		t.Fatalf("failure must keep the step, show why, and not mark changed:\n%s", plain(m))
	}
}

func TestTeamStepEngineSaveFailureIsReportedAfterTheTeamExists(t *testing.T) {
	f := newTeamFixture(t)
	f.env.SetEngine = func(string) error { return errors.New("config is read-only") }
	m := f.toTeam(t)
	key(m, "enter")
	v := plain(m)
	if m.steps[m.idx].ID() != "team" || !strings.Contains(v, "team created, but the default engine was not saved") {
		t.Fatalf("engine failure not surfaced:\n%s", v)
	}
	if !f.env.Changed {
		t.Fatal("the team was created, so the run changed launch-time state")
	}
}

func TestTeamStepWithoutAnyCLIExplainsAndStillWorks(t *testing.T) {
	f := newTeamFixture(t)
	f.env.DetectCLIs = func() map[string]string { return map[string]string{"claude": ""} }
	m := f.toTeam(t)
	if v := plain(m); !strings.Contains(v, "No coding CLI found") {
		t.Fatalf("no-CLI guidance missing:\n%s", v)
	}
	key(m, "enter")
	if len(f.applied) != 1 || len(f.engines) != 0 {
		t.Fatalf("applied=%v engines=%v", f.applied, f.engines)
	}
}

func TestGoldenTeamStep(t *testing.T) {
	f := newTeamFixture(t)
	m := f.toTeam(t)
	golden.Snapshot(t, "wizard_team", plain(m))
}

func TestDefaultEngineFollowsPreferenceNotAlphabet(t *testing.T) {
	f := newTeamFixture(t)
	f.env.DetectCLIs = func() map[string]string {
		return map[string]string{"antigravity": "1", "codex": "1", "claude": "1", "zed-agent": "1"}
	}
	m := f.toTeam(t)
	if v := plain(m); !strings.Contains(v, "[claude]") ||
		!strings.Contains(v, "coding CLIs found: claude, codex, antigravity, zed-agent") {
		t.Fatalf("default engine must be claude, in preference order:\n%s", v)
	}
	key(m, "enter")
	if strings.Join(f.engines, ",") != "cli:claude" {
		t.Fatalf("engines = %v", f.engines)
	}
}

func TestConfiguredEngineIsPreselected(t *testing.T) {
	f := newTeamFixture(t)
	f.env.Engine = "cli:opencode"
	m := f.toTeam(t)
	key(m, "enter")
	if strings.Join(f.engines, ",") != "cli:opencode" {
		t.Fatalf("a configured engine must stay the default, got %v", f.engines)
	}
}
