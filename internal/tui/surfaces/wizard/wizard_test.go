package wizard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
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
	if v := plain(m); !strings.Contains(v, "✓ found") || !strings.Contains(v, "Ada <ada@example.com>") {
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
	if !strings.Contains(plain(m), "change it later in Settings") {
		t.Fatalf("reduced motion must not hide text behind a clock:\n%s", plain(m))
	}
	theme.MotionForTest(t, true)
	m2 := New(f.env, setup.State{}, DefaultSteps(f.env, "9.9.9")...)
	m2.Resize(100, 40)
	if strings.Contains(plain(m2), "Everything starts") || strings.Contains(plain(m2), "Welcome.") {
		t.Fatalf("with motion the text starts hidden:\n%s", plain(m2))
	}
	for i := 0; i < 60; i++ {
		m2.Update(tickMsg{})
	}
	if !strings.Contains(plain(m2), "change it later in Settings") {
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
	applied  []string
	composed []starter.Counts
	engines  []string
	roster   int
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
	f.env.ComposeTeam = func(c starter.Counts) ([]starter.Result, error) {
		f.composed = append(f.composed, c)
		return []starter.Result{{Team: "atlas-team", Created: []string{"atlas", "forge"}, TeamCreated: true}}, nil
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
	key(m, "right", "right") // custom → solo → squad
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
	key(m, "right", "right") // → squad
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
	if len(f.composed) != 1 || len(f.engines) != 0 {
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
	key(m, "right", "right") // → squad
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
	if len(f.composed) != 1 || len(f.engines) != 0 {
		t.Fatalf("applied=%v engines=%v", f.applied, f.engines)
	}
}

func TestGoldenTeamStep(t *testing.T) {
	f := newTeamFixture(t)
	m := f.toTeam(t)
	golden.Snapshot(t, "wizard_team", plain(m))
	key(m, "right", "right") // → squad: count rows hide
	golden.Snapshot(t, "wizard_team_preset", plain(m))
}

func TestTeamStepCustomCountsComposeTeams(t *testing.T) {
	f := newTeamFixture(t)
	m := f.toTeam(t)
	v := plain(m)
	for _, want := range []string{"leads", "fullstack", "testers", "1 team · 4 employees", "Atlas ★", "(fullstack)"} {
		if !strings.Contains(v, want) {
			t.Errorf("custom view lacks %q:\n%s", want, v)
		}
	}
	key(m, "tab", "tab", "right")                      // team leads 1 → 2
	key(m, "tab", "tab", "tab", "tab", "tab", "right") // PMs 0 → 1
	if v := plain(m); !strings.Contains(v, "2 teams · 6 employees") {
		t.Fatalf("summary did not follow the counts:\n%s", v)
	}
	key(m, "enter")
	want := starter.Counts{"lead": 2, "fullstack": 2, "tester": 1, "product-manager": 1}
	if len(f.composed) != 1 || !reflect.DeepEqual(f.composed[0], want) {
		t.Fatalf("composed %v, want %v", f.composed, want)
	}
	if len(f.applied) != 0 || !strings.Contains(strings.Join(f.env.Applied, "|"), "team atlas-team created") {
		t.Fatalf("applied=%v env=%v", f.applied, f.env.Applied)
	}
}

func TestTeamStepTabSkipsCountsForAPreset(t *testing.T) {
	f := newTeamFixture(t)
	m := f.toTeam(t)
	key(m, "right", "right") // squad
	step := m.steps[m.idx].(*teamStep)
	key(m, "tab", "tab")
	if step.form.Cur() != fieldTeam {
		t.Fatalf("tab landed on hidden field %d", step.form.Cur())
	}
}

func TestDefaultEngineFollowsPreferenceNotAlphabet(t *testing.T) {
	f := newTeamFixture(t)
	f.env.DetectCLIs = func() map[string]string {
		return map[string]string{"antigravity": "1", "codex": "1", "claude": "1", "zed-agent": "1"}
	}
	m := f.toTeam(t)
	if v := plain(m); !strings.Contains(v, "‹claude›") ||
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
	for _, want := range []string{"claude", "2.1.285", "! ready", "newer than verified 2.1.177",
		"codex", "· not installed", "cursor-agent", "sign in: run `claude`"} {
		if !strings.Contains(v, want) {
			t.Errorf("cli step lacks %q:\n%s", want, v)
		}
	}
	key(m, "j")
	if !strings.Contains(plain(m), "sign in: run `codex`") {
		t.Fatalf("hint did not follow the cursor:\n%s", plain(m))
	}
	// The install key names what it does for the selected row (F-098).
	if _, h := m.Hints(); !strings.Contains(strings.Join(h, ","), "i install") {
		t.Fatalf("codex row: hints %v", h)
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
	if _, h := m.Hints(); len(f.installs) != 0 || !strings.Contains(strings.Join(h, ","), "↑/↓ select") {
		t.Fatalf("cancel must return to the list without installing: %v\n%s", h, plain(m))
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

// ---- integrations step (ADR-0043: the vendors' official plugins) ----

type intFixture struct {
	*fixture
	calls  []string
	who    string
	failAs error
	added  map[string]bool
}

var officialRows = []IntegrationRow{
	{Name: "Atlassian", Plugin: "atlassian", Description: "Jira and Confluence", SignIn: "sign in with your Atlassian account on first use"},
	{Name: "Slack", Plugin: "slack", Description: "Search messages and channels", SignIn: "sign in with your Slack account on first use"},
	{Name: "Zoom", Plugin: "zoom-plugin", Description: "Meetings and chat", SignIn: "needs a Zoom access token (Settings → SECRETS)"},
}

func newIntFixture(t *testing.T) *intFixture {
	t.Helper()
	f := &intFixture{fixture: newFixture(t, true), added: map[string]bool{}}
	f.env.IntegrationRows = func() []IntegrationRow {
		var rows []IntegrationRow
		for _, r := range officialRows {
			r.Installed = f.added[r.Plugin]
			rows = append(rows, r)
		}
		return rows
	}
	f.env.SetupIntegration = func(_ context.Context, plugin, who string) (string, error) {
		f.calls = append(f.calls, plugin)
		f.who = who
		if f.failAs != nil {
			return "", f.failAs
		}
		f.added[plugin] = true
		return "atlas, quill have it", nil
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

func TestIntegrationsListsTheOfficialPlugins(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	v := plain(m)
	for _, want := range []string{"Atlassian", "Slack", "Zoom", "not added", "sign in with your Atlassian account"} {
		if !strings.Contains(v, want) {
			t.Errorf("list lacks %q:\n%s", want, v)
		}
	}
}

func TestIntegrationsAddsAPluginForTheChosenBots(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	key(m, "j", "s", "enter") // Slack → the bots it fits → add
	if len(f.calls) != 1 || f.calls[0] != "slack" || f.who != whoRecommended {
		t.Fatalf("calls=%v who=%q", f.calls, f.who)
	}
	v := plain(m)
	if !strings.Contains(v, "Slack added") || !strings.Contains(v, "added") || !f.env.Changed {
		t.Fatalf("after add (changed=%v):\n%s", f.env.Changed, v)
	}
}

func TestIntegrationsEveryBotAndFailure(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	key(m, "s", "right", "enter")
	if f.who != whoEveryone {
		t.Fatalf("who = %q", f.who)
	}
	g := newIntFixture(t)
	g.failAs = errors.New("could not reach github.com")
	m = g.toIntegrations(t)
	key(m, "s", "enter")
	if v := plain(m); !strings.Contains(v, "could not add Atlassian: could not reach github.com") || g.env.Changed {
		t.Fatalf("failure (changed=%v):\n%s", g.env.Changed, v)
	}
}

func TestIntegrationsEscStepsBackWithoutWriting(t *testing.T) {
	f := newIntFixture(t)
	m := f.toIntegrations(t)
	key(m, "s", "esc")
	if len(f.calls) != 0 || m.cur().(*integrationsStep).phase != intList {
		t.Fatal("backing out wrote something")
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
	f.added["slack"] = true
	m := f.toIntegrations(t)
	golden.Snapshot(t, "wizard_integrations", plain(m))
	key(m, "s")
	golden.Snapshot(t, "wizard_integrations_info", plain(m))
}

// ---- tour offer (F-048) ----

func TestDoneStepOffersTheTourOnlyWhenWired(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	key(m, "enter", "enter", "enter") // → done
	if v := plain(m); strings.Contains(v, "tour first") {
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
	if v := plain(m2); !strings.Contains(v, " t 2-minute tour first ") {
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

// ---- default engine for an existing roster (cloned team workspace) ----

func TestCLIStepSetsTheDefaultEngineForAClonedWorkspace(t *testing.T) {
	f := newCLIFixture(t)
	var set []string
	f.env.SetEngine = func(e string) error { set = append(set, e); return nil }
	f.installed = map[string]string{"codex": "0.147.0", "claude": "2.1.285"}
	m := f.toCLI(t)
	if !strings.Contains(plain(m), "default engine for this workspace: cli:claude") {
		t.Fatalf("the default must be visible before it is applied:\n%s", plain(m))
	}
	key(m, "enter")
	if strings.Join(set, ",") != "cli:claude" || f.env.Engine != "cli:claude" || !f.env.Changed {
		t.Fatalf("set=%v engine=%q changed=%v", set, f.env.Engine, f.env.Changed)
	}
	if !strings.Contains(strings.Join(f.env.Applied, "|"), "default engine cli:claude") {
		t.Fatalf("applied = %v", f.env.Applied)
	}
}

func TestCLIStepLeavesAConfiguredOrImpossibleEngineAlone(t *testing.T) {
	cases := map[string]func(*cliFixture){
		"already configured": func(f *cliFixture) { f.env.Engine = "cli:codex" },
		"no CLI usable":      func(f *cliFixture) { f.installed = map[string]string{} },
		"no workspace":       func(f *cliFixture) { f.env.Root = "" },
		"no way to save":     func(f *cliFixture) { f.env.SetEngine = nil },
	}
	for name, mutate := range cases {
		f := newCLIFixture(t)
		calls := 0
		f.env.SetEngine = func(string) error { calls++; return nil }
		mutate(f)
		m := f.wizard()
		key(m, "enter", "enter", "enter")
		if m.steps[m.idx].ID() != "cli" {
			continue // step not shown for this variant (e.g. no workspace → the workspace step runs)
		}
		key(m, "enter")
		if calls != 0 {
			t.Errorf("%s: SetEngine called %d time(s)", name, calls)
		}
	}
}

func TestCLIEngineSaveFailureKeepsTheStep(t *testing.T) {
	f := newCLIFixture(t)
	f.env.SetEngine = func(string) error { return errors.New("config is read-only") }
	m := f.toCLI(t)
	key(m, "enter")
	if m.steps[m.idx].ID() != "cli" || !strings.Contains(plain(m), "could not save the default engine: config is read-only") {
		t.Fatalf("failure not surfaced:\n%s", plain(m))
	}
}

func TestTeamStepStartsFromTheEngineTheCLIStepChose(t *testing.T) {
	f := newTeamFixture(t)
	f.env.SetEngine = func(e string) error { f.engines = append(f.engines, e); return nil }
	f.env.CLIStatus = func() []CLIRow {
		r := CLIRow{Name: "codex", Version: "0.147.0", Tested: "0.147.0"}
		r.Plan, _ = clirun.PlanFor("codex")
		return []CLIRow{r}
	}
	f.env.DetectCLIs = func() map[string]string { return map[string]string{"codex": "0.147.0", "claude": "2.1.0"} }
	m := f.wizard()
	key(m, "enter", "enter", "enter") // welcome, identity, conventions → cli
	key(m, "enter")                   // cli: sets cli:codex (the only usable one it lists)
	if m.steps[m.idx].ID() != "team" || f.env.Engine != "cli:codex" {
		t.Fatalf("step=%s engine=%q", m.steps[m.idx].ID(), f.env.Engine)
	}
	if !strings.Contains(plain(m), "‹codex›") {
		t.Fatalf("team step must preselect the engine already chosen:\n%s", plain(m))
	}
}

// ---- project step (F-080) ----

func projectFixture(t *testing.T) (*fixture, *[]brief.Brief) {
	t.Helper()
	f := newFixture(t, true)
	var saved []brief.Brief
	f.env.DetectBrief = func() brief.Brief {
		return brief.Brief{Name: "widget", Summary: "Turns gears into sprockets.", Stack: "Go",
			Process: brief.DefaultProcess, Testing: "Run before saying done: go test ./..."}
	}
	f.env.SaveBrief = func(b brief.Brief) error { saved = append(saved, b); return nil }
	f.env.HasBrief = func() bool { return len(saved) > 0 }
	return f, &saved
}

func TestProjectStepAcceptsTheDraftOnEnter(t *testing.T) {
	f, saved := projectFixture(t)
	m := f.wizard()
	key(m, "enter") // welcome
	if m.steps[m.idx].ID() != "project" {
		t.Fatalf("on %s, want project", m.steps[m.idx].ID())
	}
	v := plain(m)
	for _, want := range []string{"Tell your team about the project", "widget", "Turns gears", "never do"} {
		if !strings.Contains(v, want) {
			t.Errorf("project step lacks %q:\n%s", want, v)
		}
	}
	key(m, "enter")
	if len(*saved) != 1 || (*saved)[0].Name != "widget" || (*saved)[0].Stack != "Go" {
		t.Fatalf("saved %+v", *saved)
	}
	if !strings.Contains(strings.Join(f.env.Applied, "|"), "project brief saved") {
		t.Fatalf("applied %v", f.env.Applied)
	}
}

func TestProjectStepEditsAndKeepsNewlines(t *testing.T) {
	f, saved := projectFixture(t)
	m := f.wizard()
	key(m, "enter")
	for i := 0; i < 8; i++ { // → never do
		key(m, "tab")
	}
	for _, r := range "prod" {
		key(m, string(r))
	}
	key(m, "ctrl+j") // a real line break, not a typed \n
	for _, r := range "drop tables" {
		key(m, string(r))
	}
	if v := plain(m); !strings.Contains(v, "prod  +1 more") || !strings.Contains(v, "Hard lines") {
		t.Fatalf("list must summarise the lines and the field explain itself:\n%s", v)
	}
	key(m, "enter")
	if got := (*saved)[0].Dont; got != "prod\ndrop tables" {
		t.Fatalf("dont = %q", got)
	}
}

func TestProjectStepHiddenWhenABriefExists(t *testing.T) {
	f, _ := projectFixture(t)
	f.env.HasBrief = func() bool { return true }
	if strings.Contains(strings.Join(titles(f.wizard()), ","), "project") {
		t.Fatal("project step offered over an existing brief")
	}
}

func TestGoldenProjectStep(t *testing.T) {
	f, _ := projectFixture(t)
	m := f.wizard()
	key(m, "enter", "tab", "tab", "tab", "tab", "tab") // cursor on a long value
	golden.Snapshot(t, "wizard_project", plain(m))
}

// ---- you step (F-081) ----

func TestStyleStepSavesTheProfileWithDefaults(t *testing.T) {
	f := newFixture(t, true)
	var saved []settings.Profile
	f.env.SaveProfile = func(p settings.Profile) error { saved = append(saved, p); return nil }
	m := f.wizard()
	key(m, "enter", "enter") // welcome, identity (found: Ada)
	if m.steps[m.idx].ID() != "you" {
		t.Fatalf("on %s, want you", m.steps[m.idx].ID())
	}
	v := plain(m)
	for _, want := range []string{"How do you work?", "developer", "plain", "English", "Ada reads replies best"} {
		if !strings.Contains(v, want) {
			t.Errorf("you step lacks %q:\n%s", want, v)
		}
	}
	golden.Snapshot(t, "wizard_you", v)
	key(m, "tab", "right")                             // tone → formal
	key(m, "tab", "tab", "tab", "tab", "tab", "right") // as → bullets
	key(m, "enter")
	if len(saved) != 1 || saved[0].Writing.Tone != "formal" || saved[0].Reading.Format != "bullets" ||
		saved[0].Role != "developer" || saved[0].Writing.Length != "normal" || saved[0].Writing.Language != "English" {
		t.Fatalf("saved %+v", saved)
	}
	if f.env.Profile.Writing.Tone != "formal" {
		t.Fatal("env profile not updated for later steps")
	}
}

func TestStyleStepStartsFromTheSavedProfile(t *testing.T) {
	f := newFixture(t, true)
	f.env.Profile = settings.Profile{Role: "CTO", Reading: settings.ProfileReading{Detail: "detailed"}}
	f.env.SaveProfile = func(settings.Profile) error { return nil }
	m := f.wizard()
	key(m, "enter", "enter")
	step := m.steps[m.idx].(*styleStep)
	if v := step.form.Values(); v[0] != "CTO" || v[7] != "detailed" {
		t.Fatalf("values %v", v)
	}
}

// ---- conventions learned and templates (F-082) ----

func TestConventionsStepPreselectsWhatWasLearned(t *testing.T) {
	f := newFixture(t, true)
	f.env.LearnConventions = func() conventions.Inference {
		return conventions.InferCommits([]string{"feat: a", "fix: b", "docs: c", "feat: d", "chore: e"})
	}
	m := f.wizard()
	key(m, "enter", "enter") // welcome, identity
	v := plain(m)
	if !strings.Contains(v, "learned from your repo") || !strings.Contains(v, "‹conventional›") || !strings.Contains(v, "‹detected›") {
		t.Fatalf("detection not shown:\n%s", v)
	}
	golden.Snapshot(t, "wizard_conventions_learned", v)
	key(m, "enter")
	if f.savedConv == nil || f.savedConv.Commit.Format != conventions.FormatConventional {
		t.Fatalf("learned format not saved: %+v", f.savedConv)
	}
}

func TestConventionsStepTemplateSyncsTheFields(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	key(m, "enter", "enter")
	for i := 0; i < fieldTemplate; i++ {
		key(m, "tab")
	}
	key(m, "right", "right", "right") // current → default → feature → ticket
	v := plain(m)
	if !strings.Contains(v, "‹ticket›") || !strings.Contains(v, "{ticket}/{slug}") {
		t.Fatalf("template did not sync the fields:\n%s", v)
	}
	key(m, "enter")
	c := f.savedConv
	if c == nil || c.Active != "ticket" || c.Commit.Format != conventions.FormatTicket || c.Branch.Task != "{ticket}/{slug}" || c.Commit.Subject != "{ticket}: {subject}" {
		t.Fatalf("saved %+v", c)
	}
}

// ---- identity suggestions and edits (F-083) ----

func TestIdentitySuggestedWhenUnset(t *testing.T) {
	f := newFixture(t, true)
	f.env.Identity = func(context.Context) (gitcore.Identity, error) { return gitcore.Identity{}, gitcore.ErrIdentityUnset }
	f.env.SuggestIdentity = func(context.Context) []gitcore.Suggestion {
		return []gitcore.Suggestion{
			{Identity: gitcore.Identity{Name: "Ada L", Email: "ada@home.org"}, Source: "most commits in this repo"},
			{Identity: gitcore.Identity{Name: "Ada Lovelace"}, Source: "your computer account"},
		}
	}
	m := f.wizard()
	key(m, "enter")
	v := plain(m)
	for _, want := range []string{"Ada L", "ada@home.org", "suggested — from most commits in this repo", "Ada Lovelace · your computer account"} {
		if !strings.Contains(v, want) {
			t.Fatalf("lacks %q:\n%s", want, v)
		}
	}
	golden.Snapshot(t, "wizard_identity_suggested", v)
	key(m, "enter", "enter") // accept the suggestion, confirm the commands
	if len(f.setCalls) != 1 || f.setCalls[0] != (gitcore.Identity{Name: "Ada L", Email: "ada@home.org"}) {
		t.Fatalf("set %+v", f.setCalls)
	}
}

func TestIdentityFoundCanBeChanged(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	key(m, "enter")
	if v := plain(m); !strings.Contains(v, " e Change ") {
		t.Fatalf("no change hint:\n%s", v)
	}
	key(m, "e")
	if v := plain(m); !strings.Contains(v, "Change the author") || !strings.Contains(v, "Ada") {
		t.Fatalf("edit form not prefilled:\n%s", v)
	}
	key(m, "tab", "ctrl+e")
	for i := 0; i < len("ada@example.com"); i++ {
		key(m, "backspace")
	}
	typeText(m, "ada@work.io")
	key(m, "enter", "enter")
	if len(f.setCalls) != 1 || f.setCalls[0].Email != "ada@work.io" || f.setCalls[0].Name != "Ada" {
		t.Fatalf("set %+v", f.setCalls)
	}
}

// ---- catch-up (F-085) ----

func TestCatchUpOffersNewStepsOnce(t *testing.T) {
	f := newFixture(t, true)
	f.env.SaveProfile = func(settings.Profile) error { return nil }
	st := setup.State{Finished: true}
	st.Mark("welcome", time.Now())
	steps := CatchUpSteps(f.env, "9.9.9", st)
	if len(steps) != 2 || steps[0].ID() != "you" || steps[1].ID() != "done" {
		t.Fatalf("catch-up %v", titles(New(f.env, st, steps...)))
	}
	m := New(f.env, st, steps...)
	m.Resize(100, 40)
	run(m, m.Init())
	key(m, "esc") // skip "you"
	last := f.persisted[len(f.persisted)-1]
	if CatchUpSteps(f.env, "9.9.9", last) != nil {
		t.Fatalf("a skipped step is offered again: %+v", last)
	}
	st.Mark("you", time.Now())
	if CatchUpSteps(f.env, "9.9.9", st) != nil {
		t.Fatal("an applied step is offered again")
	}
}

func TestCatchUpSkipSetupSettlesEveryStep(t *testing.T) {
	f := newFixture(t, true)
	f.env.SaveProfile = func(settings.Profile) error { return nil }
	f.env.DetectBrief = func() brief.Brief { return brief.Brief{Name: "x"} }
	f.env.SaveBrief = func(brief.Brief) error { return nil }
	f.env.HasBrief = func() bool { return false }
	st := setup.State{Finished: true}
	steps := CatchUpSteps(f.env, "9.9.9", st)
	if len(steps) != 3 { // project, you, done
		t.Fatalf("catch-up %d steps", len(steps))
	}
	m := New(f.env, st, steps...)
	m.Resize(100, 40)
	run(m, m.Init())
	key(m, "ctrl+x")
	last := f.persisted[len(f.persisted)-1]
	if got := CatchUpSteps(f.env, "9.9.9", last); got != nil {
		t.Fatalf("skip setup left steps to nag about: %+v", last)
	}
}

// ---- member picker and skips that must not leak ----

func mkRepos(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkspacePickerAddsOnlyChosenRepos(t *testing.T) {
	f := newFixture(t, false)
	mkRepos(t, f.root, "api", "docs", "web")
	var got []setup.MemberSpec
	f.env.InitWorkspace = func(root string, ms []setup.MemberSpec) ([]setup.MemberSpec, error) {
		got = ms
		return setup.InitWorkspace(root, ms)
	}
	m := f.wizard()
	key(m, "enter") // → workspace
	v := plain(m)
	if !strings.Contains(v, "Found 3 repos") || !strings.Contains(v, "None selected · enter adds all 3") {
		t.Fatalf("picker expected, nothing preselected:\n%s", v)
	}
	if _, hints := m.Hints(); strings.Join(hints[:3], ",") != "space select,a all,l later" {
		t.Fatalf("picker keys missing from the status row: %v", hints)
	}
	key(m, "space", "down", "down", "space", "enter") // api, web
	if len(got) != 2 || got[0].Name != "api" || got[1].Name != "web" {
		t.Fatalf("members = %+v, want api and web", got)
	}
	ws, err := os.ReadFile(filepath.Join(f.root, ".dhi", "workspace.toml"))
	if err != nil || strings.Contains(string(ws), "docs") {
		t.Fatalf("docs joined unasked:\n%s", ws)
	}
}

func TestWorkspacePickerSelectAllAndKeepsPicksAcrossBack(t *testing.T) {
	f := newFixture(t, false)
	mkRepos(t, f.root, "api", "web")
	m := f.wizard()
	key(m, "enter", "down", "space") // web
	key(m, "ctrl+b", "enter")        // welcome and back again
	if !strings.Contains(plain(m), "1 of 2 selected") {
		t.Fatalf("picks lost on back:\n%s", plain(m))
	}
	key(m, "a")
	if !strings.Contains(plain(m), "2 of 2 selected") {
		t.Fatalf("a must select all:\n%s", plain(m))
	}
	key(m, "a")
	if !strings.Contains(plain(m), "None selected") {
		t.Fatalf("a again must clear:\n%s", plain(m))
	}
}

func TestWorkspaceSingleCandidateNeedsNoPicking(t *testing.T) {
	f := newFixture(t, false)
	mkRepos(t, f.root, "web")
	m := f.wizard()
	key(m, "enter")
	if strings.Contains(plain(m), "selected") {
		t.Fatalf("one repo: nothing to choose:\n%s", plain(m))
	}
	key(m, "enter")
	if f.env.Root != f.root {
		t.Fatal("single repo not created")
	}
}

func TestGoldenWorkspacePicker(t *testing.T) {
	f := newFixture(t, false)
	mkRepos(t, f.root, "api", "web")
	f.env.CWD = "/work/acme" // the temp dir differs per run
	f.env.Discover = func(string) []setup.MemberSpec { return setup.DiscoverMembers(f.root) }
	m := f.wizard()
	key(m, "enter", "space")
	golden.Snapshot(t, "wizard_workspace_picker", plain(m))
}

// Skipping setup in a bare directory must not settle the steps that only
// run in a workspace: once one exists, catch-up still offers them.
func TestSkipSetupWithoutWorkspaceLeavesWorkspaceStepsOwed(t *testing.T) {
	f := newTeamFixture(t)
	f.env.Root = ""
	f.env.DetectBrief = func() brief.Brief { return brief.Brief{Name: "x"} }
	f.env.SaveBrief = func(brief.Brief) error { return nil }
	f.env.HasBrief = func() bool { return false }
	m := f.wizard()
	key(m, "ctrl+x")
	last := f.persisted[len(f.persisted)-1]
	for _, id := range []string{"project", "team"} {
		if last.Settled(id) {
			t.Fatalf("%s settled without ever being offered: %+v", id, last)
		}
	}
	f.env.Root = f.root // the workspace appears later
	var ids []string
	for _, s := range CatchUpSteps(f.env, "9.9.9", last) {
		ids = append(ids, s.ID())
	}
	if strings.Join(ids, ",") != "project,team,done" {
		t.Fatalf("catch-up = %v, want project, team", ids)
	}
}

func TestDoneStepNamesSkippedSteps(t *testing.T) {
	f := newFixture(t, false)
	m := f.wizard()
	key(m, "enter", "esc", "esc") // welcome, skip workspace, skip identity
	key(m, "enter")               // conventions
	v := plain(m)
	if !strings.Contains(v, "skipped: workspace, identity") || !strings.Contains(v, "no workspace yet") {
		t.Fatalf("done must name what was skipped:\n%s", v)
	}
	key(m, "ctrl+b", "ctrl+b", "enter") // back to identity, apply it
	key(m, "enter")
	if v := plain(m); !strings.Contains(v, "skipped: workspace") || strings.Contains(v, "identity —") {
		t.Fatalf("an applied step left the skipped list:\n%s", v)
	}
}

func TestIdentityCheckCanBeSkipped(t *testing.T) {
	f := newFixture(t, true)
	m := f.wizard()
	key(m, "enter") // → identity
	step := m.steps[m.idx].(*identityStep)
	step.phase = idChecking // git still answering
	m.HandleKey("esc")
	if m.steps[m.idx].ID() != "conventions" {
		t.Fatalf("esc while checking must skip; step = %s", m.steps[m.idx].ID())
	}
	m.Update(identityMsg{id: gitcore.Identity{Name: "late"}}) // the late answer is ignored
	if m.steps[m.idx].ID() != "conventions" || f.env.WhoName == "late" {
		t.Fatal("a late identity answer disturbed the next step")
	}
}

func TestWorkspacePickerWindowsALongList(t *testing.T) {
	f := newFixture(t, false)
	var names []string
	for i := 0; i < 30; i++ {
		names = append(names, "repo"+string(rune('a'+i/10))+string(rune('0'+i%10)))
	}
	mkRepos(t, f.root, names...)
	m := f.wizard()
	m.Resize(100, 34)
	key(m, "enter")
	for i := 0; i < 29; i++ {
		key(m, "down")
	}
	key(m, "space")
	v := plain(m)
	for _, want := range []string{"✓ welcome", "Found 30 repos", "↑ 20 more", "▌ ✓ repoc9", "1 of 30 selected", "╰"} {
		if !strings.Contains(v, want) {
			t.Fatalf("long list lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "repoa0") {
		t.Fatalf("rows outside the window rendered:\n%s", v)
	}
}

// No choice made: the default (every repo found) is used.
func TestWorkspacePickerEnterWithNoneTakesAll(t *testing.T) {
	f := newFixture(t, false)
	mkRepos(t, f.root, "api", "web")
	var got []setup.MemberSpec
	f.env.InitWorkspace = func(root string, ms []setup.MemberSpec) ([]setup.MemberSpec, error) {
		got = ms
		return setup.InitWorkspace(root, ms)
	}
	m := f.wizard()
	key(m, "enter", "enter")
	if len(got) != 2 || f.env.Root != f.root {
		t.Fatalf("members = %+v root=%q, want every repo", got, f.env.Root)
	}
}

// "Later" means later: nothing is created and no default is applied.
func TestWorkspacePickerLaterCreatesNothing(t *testing.T) {
	f := newFixture(t, false)
	mkRepos(t, f.root, "api", "web")
	called := false
	f.env.InitWorkspace = func(string, []setup.MemberSpec) ([]setup.MemberSpec, error) {
		called = true
		return nil, nil
	}
	m := f.wizard()
	key(m, "enter", "space", "l")
	if called || f.env.Root != "" || f.env.Changed {
		t.Fatalf("later created a workspace (called=%v root=%q)", called, f.env.Root)
	}
	if m.steps[m.idx].ID() != "identity" || !m.skipped["workspace"] {
		t.Fatalf("later must move on without the workspace; step=%s", m.steps[m.idx].ID())
	}
	if _, err := os.Stat(filepath.Join(f.root, ".dhi")); !os.IsNotExist(err) {
		t.Fatal(".dhi written although the user chose later")
	}
}
