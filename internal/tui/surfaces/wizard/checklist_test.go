package wizard

import (
	"context"
	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/setup"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/kit"
)

func states(items []kit.ChecklistItem) string {
	var b []string
	for _, it := range items {
		mark := map[kit.ChecklistState]string{kit.ItemPending: "○", kit.ItemDone: "✓", kit.ItemSkipped: "–"}[it.State]
		b = append(b, mark+it.ID)
	}
	return strings.Join(b, " ")
}

// F-098: the checklist reads per-step records, lists what applies, and
// migrates a setup finished before it existed without nagging.
func TestChecklistStates(t *testing.T) {
	f := newFixture(t, false)
	if got := states(Checklist(f.env, "9.9.9", setup.State{})); got != "○workspace ○identity ○conventions" {
		t.Fatalf("fresh, no workspace: %s", got)
	}

	g := newFixture(t, true)
	st := setup.State{}
	st.Mark("identity", g.env.now())
	st.MarkSkipped("conventions", g.env.now())
	if got := states(Checklist(g.env, "9.9.9", st)); got != "✓identity –conventions" {
		t.Fatalf("records: %s", got)
	}

	// Finished, no records: those steps never ran, so they are settled
	// as skipped (never claimed done); catch-up steps that apply stay
	// pending. A returning user in a new workspace is the common case.
	h := newFixture(t, true)
	h.env.SaveProfile = func(settings.Profile) error { return nil } // "you" applies: a catch-up step
	h.env.IntegrationRows = func() []IntegrationRow { return nil }
	h.env.SetupIntegration = func(context.Context, string, string) (string, error) { return "", nil }
	if got := states(Checklist(h.env, "9.9.9", setup.State{Finished: true})); got != "–identity ○you –conventions –integrations" {
		t.Fatalf("migrated: %s", got)
	}
}

// Running one step records that step only and never finishes setup.
func TestSingleStepRecordsOnlyItself(t *testing.T) {
	f := newFixture(t, true)
	closed := 0
	m := Single(f.env, "9.9.9", setup.State{}, "identity", func() { closed++ })
	m.Resize(100, 40)
	run(m, m.Init())
	if v := plain(m); !strings.Contains(v, "Git identity") || strings.Contains(v, "welcome") {
		t.Fatalf("single dialog must head with the task, no stepper:\n%s", v)
	}
	key(m, "enter") // identity found → use it
	if !m.Finished() || closed != 1 || m.NeedsRelaunch() {
		t.Fatalf("finished=%v closed=%d relaunch=%v", m.Finished(), closed, m.NeedsRelaunch())
	}
	last := f.persisted[len(f.persisted)-1]
	if !last.Has("identity") || last.Finished || last.Has("conventions") {
		t.Fatalf("persisted %+v", last)
	}
}

func TestSingleStepEscSkipsOnlyItself(t *testing.T) {
	f := newFixture(t, true)
	m := Single(f.env, "9.9.9", setup.State{}, "conventions", nil)
	m.Resize(100, 40)
	run(m, m.Init())
	if _, hints := m.Hints(); !strings.HasSuffix(strings.Join(hints, ","), "enter save,esc skip") {
		t.Fatalf("hints = %v", hints)
	}
	key(m, "esc")
	last := f.persisted[len(f.persisted)-1]
	if !m.Finished() || last.Finished || !last.Settled("conventions") || last.Has("conventions") {
		t.Fatalf("finished=%v persisted %+v", m.Finished(), last)
	}
}

func TestSingleRefusesAStepThatDoesNotApply(t *testing.T) {
	f := newFixture(t, true) // a workspace exists: its step no longer applies
	if Single(f.env, "9.9.9", setup.State{}, "workspace", nil) != nil {
		t.Fatal("workspace step must not run inside a workspace")
	}
	if Single(f.env, "9.9.9", setup.State{}, "nope", nil) != nil {
		t.Fatal("unknown id")
	}
}

// A single step that changes launch-time state relaunches instead of
// handing back (creating the workspace).
func TestSingleWorkspaceStepRelaunches(t *testing.T) {
	f := newFixture(t, false)
	m := Single(f.env, "9.9.9", setup.State{}, "workspace", nil)
	m.Resize(100, 40)
	run(m, m.Init())
	key(m, "enter")
	if !m.NeedsRelaunch() {
		t.Fatalf("creating a workspace must relaunch; view:\n%s", plain(m))
	}
	if last := f.persisted[len(f.persisted)-1]; last.Finished || !last.Has("workspace") {
		t.Fatalf("persisted %+v", last)
	}
}

func TestGoldenSingleStepDialogs(t *testing.T) {
	f := newFixture(t, true)
	f.env.Instructions = func() ([]string, []string) { return nil, nil }
	f.env.SaveInstructions = func([]string, bool) error { return nil }
	m := Single(f.env, "9.9.9", setup.State{}, "instructions", nil)
	m.Resize(100, 34)
	run(m, m.Init())
	key(m, " ", "j", "a")
	for _, r := range "Use British English" {
		key(m, string(r))
	}
	golden.Snapshot(t, "wizard_single_instructions", plain(m))
}

// F-108: a configured step can be run again from the checklist; it opens
// with what was saved, and esc leaves it done (not skipped).
func TestReRunningADoneStepIsPrefilledAndEscKeepsIt(t *testing.T) {
	f, saved := projectFixture(t)
	*saved = append(*saved, brief.Brief{Name: "widget"}) // a brief exists: the step no longer applies to a first run
	f.env.LoadBrief = func() (brief.Brief, bool) {
		return brief.Brief{Name: "widget", Summary: "The saved summary."}, true
	}
	var persisted []setup.State
	f.env.Persist = func(_ string, st setup.State) error { persisted = append(persisted, st); return nil }
	st := setup.State{}
	st.Mark("project", time.Now())
	m := Single(f.env, "9.9.9", st, "project", nil)
	if m == nil {
		t.Fatal("a done project step must be re-runnable")
	}
	m.Resize(100, 40)
	run(m, m.Init())
	if v := plain(m); !strings.Contains(v, "The saved summary.") {
		t.Fatalf("re-run must start from the saved brief:\n%s", v)
	}
	if _, h := m.Hints(); !strings.Contains(strings.Join(h, ","), "esc leave as is") {
		t.Fatalf("hints = %v", h)
	}
	key(m, "esc")
	for _, p := range persisted {
		if _, skipped := p.Skipped["project"]; skipped {
			t.Fatal("esc on a re-run must not mark the step skipped")
		}
	}
	if Single(f.env, "9.9.9", st, "workspace", nil) != nil {
		t.Fatal("creating a workspace cannot re-run inside one")
	}
}
