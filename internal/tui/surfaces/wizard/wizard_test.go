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

	"github.com/drjzlyan/dhi/internal/agentkit/catalog"
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
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

// ---- coding-CLI step (F-046) ----

type cliFixture struct {
	*fixture
	installs  []string
	installed map[string]string // name → version once "installed"
	installFn func(string) error
}

func newCLIFixture(t *testing.T) *cliFixture {
	t.Helper()
	f := &cliFixture{fixture: newFixture(t, true), installed: map[string]string{"claude": "2.1.285"}}
	f.env.CLIPrefix = func(n string) string { return "/dhi/clis/" + n }
	f.env.CLIStatus = func() []CLIRow {
		var rows []CLIRow
		for _, name := range []string{"claude", "codex", "cursor-agent"} {
			r := CLIRow{Name: name, Version: f.installed[name], Tested: "2.1.177"}
			r.Plan, _ = clirun.PlanFor(name)
			if r.Version != "" {
				r.Verdict, r.Why = clirun.Assess("2.1.177", r.Version)
			}
			rows = append(rows, r)
		}
		return rows
	}
	f.env.InstallCLI = func(_ context.Context, name string) error {
		f.installs = append(f.installs, name)
		if f.installFn != nil {
			if err := f.installFn(name); err != nil {
				return err
			}
		}
		f.installed[name] = "0.147.0"
		return nil
	}
	return f
}

func (f *cliFixture) toCLI(t *testing.T) *Model {
	t.Helper()
	m := f.wizard()
	key(m, "enter", "enter", "enter") // welcome, identity, conventions
	if m.steps[m.idx].ID() != "cli" {
		t.Fatalf("on %s, want cli:\n%s", m.steps[m.idx].ID(), plain(m))
	}
	return m
}

func TestCLIStepListsStatusAndSignInHint(t *testing.T) {
	f := newCLIFixture(t)
	m := f.toCLI(t)
	v := plain(m)
	for _, want := range []string{"claude", "2.1.285", "ready", "newer than verified 2.1.177",
		"codex", "not installed · i installs it", "cursor-agent", "not installed · i shows how",
		"sign in: run `claude`"} {
		if !strings.Contains(v, want) {
			t.Errorf("cli step lacks %q:\n%s", want, v)
		}
	}
	key(m, "j")
	if !strings.Contains(plain(m), "sign in: run `codex`") {
		t.Fatalf("hint did not follow the cursor:\n%s", plain(m))
	}
}

func TestCLIInstallShowsTheExactCommandAndCancelDoesNothing(t *testing.T) {
	f := newCLIFixture(t)
	m := f.toCLI(t)
	key(m, "j", "i")
	v := plain(m)
	if !strings.Contains(v, "npm install --prefix /dhi/clis/codex @openai/codex") ||
		!strings.Contains(v, "no sudo, nothing global") {
		t.Fatalf("exact command not shown:\n%s", v)
	}
	if len(f.installs) != 0 {
		t.Fatal("installed before confirmation")
	}
	key(m, "esc")
	if len(f.installs) != 0 || !strings.Contains(plain(m), "↑/↓ select") {
		t.Fatalf("cancel must return to the list without installing:\n%s", plain(m))
	}
}

func TestCLIInstallRunsRefreshesAndFlagsRelaunch(t *testing.T) {
	f := newCLIFixture(t)
	m := f.toCLI(t)
	key(m, "j", "i", "enter")
	if strings.Join(f.installs, ",") != "codex" {
		t.Fatalf("installs = %v", f.installs)
	}
	v := plain(m)
	if !strings.Contains(v, "codex installed") || strings.Contains(v, "not installed · i installs it") {
		t.Fatalf("list not refreshed after install:\n%s", v)
	}
	if !f.env.Changed || !strings.Contains(strings.Join(f.env.Applied, "|"), "installed codex (@openai/codex)") {
		t.Fatalf("changed=%v applied=%v", f.env.Changed, f.env.Applied)
	}
}

func TestCLIInstallFailureIsShownAndNothingIsMarkedChanged(t *testing.T) {
	f := newCLIFixture(t)
	f.installFn = func(string) error { return errors.New("npm ERR! 404 Not Found") }
	m := f.toCLI(t)
	key(m, "j", "i", "enter")
	v := plain(m)
	if !strings.Contains(v, "installing codex failed: npm ERR! 404 Not Found") || f.env.Changed {
		t.Fatalf("failure not reported cleanly (changed=%v):\n%s", f.env.Changed, v)
	}
	if m.steps[m.idx].ID() != "cli" {
		t.Fatal("a failed install must keep the step")
	}
}

func TestCLIStepBlocksNavigationWhileInstalling(t *testing.T) {
	f := newCLIFixture(t)
	m := f.toCLI(t)
	key(m, "j", "i")
	m.HandleKey("enter") // confirm: starts, but we do NOT drain the command yet
	if !m.cur().(*cliStep).Busy() {
		t.Fatal("step should be busy during the install")
	}
	m.HandleKey("ctrl+x")
	m.HandleKey("ctrl+b")
	m.HandleKey("esc")
	if m.Finished() || m.steps[m.idx].ID() != "cli" {
		t.Fatal("navigation must be ignored while an install is running")
	}
	run(m, m.TakeCmd()) // let it finish
	if m.cur().(*cliStep).Busy() {
		t.Fatal("still busy after the result")
	}
}

func TestCLIManualInstallShowsVendorCommandAndNeverRunsIt(t *testing.T) {
	f := newCLIFixture(t)
	m := f.toCLI(t)
	key(m, "j", "j", "i")
	v := plain(m)
	for _, want := range []string{"Install cursor-agent yourself", "curl https://cursor.com/install -fsS | bash",
		"https://cursor.com/docs/cli/overview"} {
		if !strings.Contains(v, want) {
			t.Errorf("manual view lacks %q:\n%s", want, v)
		}
	}
	if len(f.installs) != 0 {
		t.Fatal("a manual CLI must never be installed by DHI")
	}
	f.installed["cursor-agent"] = "2026.09.26"
	key(m, "esc", "r")
	if !strings.Contains(plain(m), "2026.09.26") {
		t.Fatalf("r should re-detect:\n%s", plain(m))
	}
}

func TestCLIAlreadyInstalledAndNoCLIReady(t *testing.T) {
	f := newCLIFixture(t)
	m := f.toCLI(t)
	key(m, "i")
	if !strings.Contains(plain(m), "claude is already installed") || len(f.installs) != 0 {
		t.Fatalf("installed CLI must not be reinstalled:\n%s", plain(m))
	}

	g := newCLIFixture(t)
	g.installed = map[string]string{}
	m2 := g.toCLI(t)
	if !strings.Contains(plain(m2), "No CLI is ready yet") {
		t.Fatalf("missing-everything warning absent:\n%s", plain(m2))
	}
}

func TestCLIStepHiddenWithoutStatusAndGuidedWithoutInstaller(t *testing.T) {
	f := newFixture(t, true)
	if strings.Contains(strings.Join(titles(f.wizard()), ","), "coding CLI") {
		t.Fatal("step shown with no CLIStatus")
	}
	g := newCLIFixture(t)
	g.env.InstallCLI = nil // no toolchain
	m := g.toCLI(t)
	key(m, "j", "i")
	if !strings.Contains(plain(m), "Install codex yourself") {
		t.Fatalf("without an installer an npm CLI must fall back to guidance:\n%s", plain(m))
	}
}

func TestGoldenCLIStep(t *testing.T) {
	f := newCLIFixture(t)
	m := f.toCLI(t)
	golden.Snapshot(t, "wizard_cli", plain(m))
	key(m, "j", "i")
	golden.Snapshot(t, "wizard_cli_confirm", plain(m))
}

// ---- integrations step (F-047) ----

type intFixture struct {
	*fixture
	calls   []string
	inputs  map[string]string
	who     string
	failAs  error
	enabled []string
	ready   map[string]bool
}

func newIntFixture(t *testing.T) *intFixture {
	t.Helper()
	f := &intFixture{fixture: newFixture(t, true), ready: map[string]bool{}, enabled: []string{"atlas", "forge"}}
	f.env.CredentialsPath = "/home/me/.config/dhi/credentials.toml"
	f.env.IntegrationRows = func() []IntegrationRow {
		var rows []IntegrationRow
		for _, e := range catalog.Entries() {
			st := catalog.State{}
			if f.ready[e.Slug] {
				st = catalog.State{Installed: true}
			}
			rows = append(rows, IntegrationRow{Entry: e, State: st})
		}
		return rows
	}
	f.env.SetupIntegration = func(slug string, in map[string]string, who string) ([]string, error) {
		f.calls = append(f.calls, slug)
		f.inputs, f.who = in, who
		if f.failAs != nil {
			return nil, f.failAs
		}
		f.ready[slug] = true
		return f.enabled, nil
	}
	return f
}

func (f *intFixture) toIntegrations(t *testing.T) *Model {
	t.Helper()
	m := f.wizard()
	key(m, "enter", "enter", "enter") // welcome, identity, conventions
	if m.steps[m.idx].ID() != "integrations" {
		t.Fatalf("on %s, want integrations:\n%s", m.steps[m.idx].ID(), plain(m))
	}
	return m
}

// selectEntry moves the cursor to a catalog slug.
func selectEntry(t *testing.T, m *Model, slug string) {
	t.Helper()
	for i, e := range catalog.Entries() {
		if e.Slug == slug {
			for j := 0; j < i; j++ {
				key(m, "j")
			}
			return
		}
	}
	t.Fatalf("no catalog entry %q", slug)
}

func TestIntegrationsListShowsEveryEntryAndTeamsIsUnavailable(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	v := plain(m)
	for _, want := range []string{"Jira + Confluence", "GitHub", "Linear", "Notion", "Slack", "Microsoft Teams", "not connected · s sets it up"} {
		if !strings.Contains(v, want) {
			t.Errorf("list lacks %q:\n%s", want, v)
		}
	}
	selectEntry(t, m, "teams")
	key(m, "s")
	v = plain(m)
	if !strings.Contains(v, "Entra ID") || m.cur().(*integrationsStep).phase != intList {
		t.Fatalf("an unavailable entry must explain itself and not open a setup:\n%s", v)
	}
}

func TestIntegrationsFullFlowMasksTheSecretAndNamesTheStorage(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	selectEntry(t, m, "linear")
	const secret = "lin_api_SuperSecret123"

	var seen []string
	snap := func() { seen = append(seen, plain(m)) }

	key(m, "s")
	snap()
	if v := plain(m); !strings.Contains(v, "run by the vendor") || !strings.Contains(v, "https://linear.app/settings/account/security") {
		t.Fatalf("info screen:\n%s", v)
	}
	key(m, "enter") // → fields
	typeText(m, secret)
	snap()
	if v := plain(m); strings.Contains(v, "SuperSecret") || !strings.Contains(v, strings.Repeat("•", len(secret))) {
		t.Fatalf("the secret must be masked while typing:\n%s", v)
	}
	key(m, "enter") // → who
	snap()
	key(m, "enter") // → confirm
	snap()
	v := plain(m)
	for _, want := range []string{"/home/me/.config/dhi/credentials.toml", "owner-only", "readable plain text",
		".dhi/mcp/linear.toml", "no secrets in it", "every employee"} {
		if !strings.Contains(v, want) {
			t.Errorf("confirm lacks %q:\n%s", want, v)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("wrote before confirmation")
	}
	key(m, "enter") // connect
	snap()

	if strings.Join(f.calls, ",") != "linear" || f.inputs["LINEAR_API_KEY"] != secret || f.who != whoEveryone {
		t.Fatalf("setup call = %v inputs=%v who=%q", f.calls, f.inputs, f.who)
	}
	if !f.env.Changed || !strings.Contains(strings.Join(f.env.Applied, "|"), "connected Linear (atlas, forge)") {
		t.Fatalf("changed=%v applied=%v", f.env.Changed, f.env.Applied)
	}
	for i, screen := range seen {
		if strings.Contains(screen, "SuperSecret") {
			t.Errorf("the secret was rendered on screen %d:\n%s", i, screen)
		}
	}
	if !strings.Contains(plain(m), "Linear connected") {
		t.Fatalf("no success message:\n%s", plain(m))
	}
}

func TestIntegrationsValidateBeforeAnyWrite(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	selectEntry(t, m, "atlassian")
	key(m, "s", "enter")
	typeText(m, "acme")
	key(m, "tab")
	typeText(m, "not-an-email")
	key(m, "tab")
	typeText(m, "tok")
	key(m, "enter")
	if v := plain(m); !strings.Contains(v, "email must look like") || m.cur().(*integrationsStep).phase != intFields {
		t.Fatalf("a bad email must stay in the form with the reason:\n%s", v)
	}
	key(m, "shift+tab") // token → email
	for i := 0; i < len("not-an-email"); i++ {
		key(m, "backspace")
	}
	typeText(m, "me@acme.com")
	key(m, "enter") // → who
	if m.cur().(*integrationsStep).phase != intWho {
		t.Fatalf("valid inputs should advance:\n%s", plain(m))
	}
	if len(f.calls) != 0 {
		t.Fatal("validation must happen before any write")
	}

	g := newIntFixture(t)
	m2 := g.toIntegrations(t)
	selectEntry(t, m2, "linear")
	key(m2, "s", "enter", "enter") // submit with the key empty
	if !strings.Contains(plain(m2), "required") {
		t.Fatalf("an empty secret must be refused:\n%s", plain(m2))
	}
}

func TestIntegrationsEscStepsBackWithoutWriting(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	selectEntry(t, m, "linear")
	key(m, "s", "enter")
	typeText(m, "tok")
	key(m, "enter", "enter") // who → confirm
	st := m.cur().(*integrationsStep)
	for _, want := range []intPhase{intWho, intFields, intInfo, intList} {
		key(m, "esc")
		if st.phase != want {
			t.Fatalf("phase = %v, want %v", st.phase, want)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("backing out wrote something")
	}
}

func TestIntegrationsFailureIsReportedAndNotMarkedChanged(t *testing.T) {
	f := newIntFixture(t)
	f.failAs = errors.New("credentials file is not valid TOML")
	m := f.toIntegrations(t)
	selectEntry(t, m, "notion")
	key(m, "s", "enter")
	typeText(m, "ntn_x")
	key(m, "enter", "enter", "enter")
	v := plain(m)
	if !strings.Contains(v, "could not connect Notion: credentials file is not valid TOML") || f.env.Changed {
		t.Fatalf("failure (changed=%v):\n%s", f.env.Changed, v)
	}
}

func TestIntegrationsNobodyYetSaysSo(t *testing.T) {
	f := newIntFixture(t)
	f.enabled = nil
	m := f.toIntegrations(t)
	selectEntry(t, m, "github")
	key(m, "s", "enter")
	typeText(m, "github_pat_x")
	key(m, "enter", "right", "right", "enter", "enter") // who → "nobody yet" → confirm → connect
	if f.who != whoNobody || !strings.Contains(plain(m), "no employee has access yet") {
		t.Fatalf("who=%q:\n%s", f.who, plain(m))
	}
}

func TestIntegrationsStepHiddenWithoutWorkspaceOrHooks(t *testing.T) {
	f := newFixture(t, true)
	if strings.Contains(strings.Join(titles(f.wizard()), ","), "integrations") {
		t.Fatal("shown without hooks")
	}
	g := newIntFixture(t)
	g.env.Root = ""
	if strings.Contains(strings.Join(titles(g.wizard()), ","), "integrations") {
		t.Fatal("shown with no workspace")
	}
}

func TestGoldenIntegrations(t *testing.T) {
	f := newIntFixture(t)
	f.ready["notion"] = true
	m := f.toIntegrations(t)
	golden.Snapshot(t, "wizard_integrations", plain(m))
	selectEntry(t, m, "atlassian")
	key(m, "s")
	golden.Snapshot(t, "wizard_integrations_info", plain(m))
}

// ---- tour offer (F-048) ----

func TestDoneStepOffersTheTourOnlyWhenWired(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	key(m, "enter", "enter", "enter") // → done
	if v := plain(m); strings.Contains(v, "Press t") {
		t.Fatalf("tour offered with no hook:\n%s", v)
	}
	key(m, "t") // ignored without a hook: the done step takes enter only
	if m.Finished() {
		t.Fatal("'t' finished the wizard with no tour wired")
	}

	g := newFixture(t, true)
	g.env.StartTour = func() {}
	m2 := g.wizard()
	key(m2, "enter", "enter", "enter")
	if v := plain(m2); !strings.Contains(v, "Press t for a 2-minute guided tour first.") {
		t.Fatalf("tour offer missing:\n%s", v)
	}
}

func TestTourStartsInProcessWhenNothingNeedsARelaunch(t *testing.T) {
	f := newFixture(t, true)
	started, queued := 0, 0
	f.env.StartTour = func() { started++ }
	f.env.QueueTour = func() error { queued++; return nil }
	m := f.wizard()
	key(m, "enter", "enter", "enter", "t")
	if !m.Finished() || m.NeedsRelaunch() || started != 1 || queued != 0 {
		t.Fatalf("finished=%v relaunch=%v started=%d queued=%d", m.Finished(), m.NeedsRelaunch(), started, queued)
	}
}

func TestTourIsQueuedAcrossARelaunch(t *testing.T) {
	f := newFixture(t, false) // creating a workspace forces a relaunch
	started, queued := 0, 0
	f.env.StartTour = func() { started++ }
	f.env.QueueTour = func() error { queued++; return nil }
	m := f.wizard()
	key(m, "enter", "enter") // welcome, create the workspace
	key(m, "ctrl+x")         // skip the rest → finish
	if !m.NeedsRelaunch() {
		t.Fatal("expected a relaunch")
	}
	// Asking for the tour at the done step queues it instead of starting it.
	g := newFixture(t, false)
	s2, q2 := 0, 0
	g.env.StartTour = func() { s2++ }
	g.env.QueueTour = func() error { q2++; return nil }
	m2 := g.wizard()
	key(m2, "enter", "enter", "enter", "enter", "enter") // welcome, workspace, identity, conventions
	if m2.steps[m2.idx].ID() != "done" {
		t.Fatalf("on %s", m2.steps[m2.idx].ID())
	}
	key(m2, "t")
	if !m2.NeedsRelaunch() || q2 != 1 || s2 != 0 {
		t.Fatalf("relaunch=%v queued=%d started=%d", m2.NeedsRelaunch(), q2, s2)
	}
	if started != 0 || queued != 0 {
		t.Fatal("declining the tour must not queue it")
	}
}
