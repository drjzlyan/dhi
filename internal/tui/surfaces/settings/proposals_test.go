package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/brief"
	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/agentkit/setprop"
	"github.com/drjzlyan/dhi/internal/ansi"
	dhisettings "github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/theme"
)

func proposalsSurface(t *testing.T) (*Model, *forge.Store) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	m, ws, _, _ := agentSurface(t)
	st := forge.Open(ws.Root)
	m.d.Forge = st
	propose := func(tl forge.Tool) {
		t.Helper()
		if _, err := st.Propose(tl, nil); err != nil {
			t.Fatal(err)
		}
	}
	propose(forge.Tool{Name: "check_all", Description: "Vet and race-test every package", Scope: "exec", ProposedBy: "dev",
		Rationale: "ran both in 5 of my last 7 tasks",
		Steps:     []forge.Step{{Run: []string{"go", "vet", "./..."}}, {Run: []string{"go", "test", "-race", "./..."}}}})
	if _, err := st.Approve("check_all", "you"); err != nil {
		t.Fatal(err)
	}
	propose(forge.Tool{Name: "race_test", Description: "Run one package's tests with -race", Scope: "exec", ProposedBy: "dev",
		Rationale: "ran it in 4 of 6 tasks",
		Inputs:    []forge.Input{{Name: "pkg", Type: "string", Required: true, Description: "package path"}},
		Steps:     []forge.Step{{Run: []string{"go", "test", "-race", "{{pkg}}"}}}})
	for m.sec != secProposals {
		feed(m, "]")
	}
	m.Resize(120, 26)
	return m, st
}

func TestProposalsGolden(t *testing.T) {
	m, _ := proposalsSurface(t)
	golden.Snapshot(t, "settings_proposals", m.View())
	golden.AssertBgIntegrity(t, "proposals", m.View())
}

func TestProposalsApproveViewArchive(t *testing.T) {
	m, st := proposalsSurface(t)
	view := ansi.Strip(m.View())
	for _, want := range []string{"PENDING", "race_test", "FORGED TOOLS", "custom__check_all", "0 calls"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	feed(m, "v") // the full definition before approving
	if v := ansi.Strip(m.View()); !strings.Contains(v, "1. go test -race {{pkg}}") || !strings.Contains(v, "why: ran it in 4 of 6 tasks") {
		t.Fatalf("definition dialog:\n%s", v)
	}
	feed(m, "esc")
	feed(m, "y")
	if !strings.Contains(m.flash, "custom__race_test") {
		t.Fatalf("flash = %q", m.flash)
	}
	if act, _ := st.Active(); len(act) != 2 {
		t.Fatalf("active = %d", len(act))
	}
	// Archive the first forged tool.
	m.propCur = 0
	feed(m, "x")
	if act, _ := st.Active(); len(act) != 1 || !strings.Contains(m.flash, "archived") {
		t.Fatalf("archive: %d active, flash %q", len(act), m.flash)
	}
}

func TestProposalsReject(t *testing.T) {
	m, st := proposalsSurface(t)
	feed(m, "n")
	if pend, _ := st.Pending(); len(pend) != 0 || !strings.Contains(m.flash, "rejected race_test") {
		t.Fatalf("pending = %d flash = %q", len(pend), m.flash)
	}
}

// F-070: an agent's settings proposal is applied as one key to the
// workspace layer by y, and rejected by n.
func TestSettingsProposalApplyAndReject(t *testing.T) {
	m, _ := proposalsSurface(t)
	ws := m.d.WS.Root
	m.d.SetProps = setprop.Open(ws)
	m.d.WorkspaceConfig = filepath.Join(ws, ".dhi", "config.toml")
	m.d.UserConfig = filepath.Join(t.TempDir(), "user.toml")
	if _, err := m.d.SetProps.Propose(setprop.Proposal{Key: "budgets.task_usd", Value: "5", Old: "0", Agent: "dev", Rationale: "40-file migration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.d.SetProps.Propose(setprop.Proposal{Key: "agents.max_iterations", Value: "5", Old: "3", Agent: "dev", Rationale: "flaky suite"}); err != nil {
		t.Fatal(err)
	}
	m.propCur = 0
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "budgets.task_usd") || !strings.Contains(view, "0 → 5") {
		t.Fatalf("setting proposal not listed:\n%s", view)
	}
	feed(m, "v")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "why: 40-file migration") || !strings.Contains(v, "applies exactly this one key") {
		t.Fatalf("proposal dialog:\n%s", v)
	}
	feed(m, "esc", "y")
	if !strings.Contains(m.flash, "applied budgets.task_usd = 5 (workspace layer)") || m.cfg.Budgets.TaskUSD != 5 {
		t.Fatalf("apply: flash %q, live %v", m.flash, m.cfg.Budgets.TaskUSD)
	}
	c, err := dhisettings.Load(m.d.UserConfig, m.d.WorkspaceConfig)
	if err != nil || c.Budgets.TaskUSD != 5 {
		t.Fatalf("not persisted: %+v %v", c.Budgets, err)
	}
	m.propCur = 0 // now the max_iterations proposal
	feed(m, "n")
	if p, _ := m.d.SetProps.Pending(); len(p) != 0 || !strings.Contains(m.flash, "rejected the agents.max_iterations proposal") {
		t.Fatalf("reject: %+v %q", p, m.flash)
	}
}

func TestProjectProposalUpdatesTheBrief(t *testing.T) {
	m, _ := proposalsSurface(t)
	ws := m.d.WS.Root
	m.d.SetProps = setprop.Open(ws)
	_ = brief.Save(ws, brief.Brief{Name: "shop", Process: "old way"})
	if _, err := m.d.SetProps.Propose(setprop.Proposal{Key: "project.process", Value: "trunk-based\nsmall PRs", Old: "old way",
		Layer: setprop.LayerProject, Agent: "atlas", Rationale: "you said so in the interview"}); err != nil {
		t.Fatal(err)
	}
	m.propCur = 0
	feed(m, "v")
	v := ansi.Strip(m.View())
	for _, want := range []string{"project brief · process", "old way", "trunk-based", "replaces this one field"} {
		if !strings.Contains(v, want) {
			t.Fatalf("dialog lacks %q:\n%s", want, v)
		}
	}
	feed(m, "esc", "y")
	b, _, _ := brief.Load(ws)
	if b.Process != "trunk-based\nsmall PRs" || b.Name != "shop" || !strings.Contains(m.flash, "project brief updated: process") {
		t.Fatalf("brief %+v flash %q", b, m.flash)
	}
}

// A long proposal wraps in the dialog: you read every word you approve
// (live, session 59: the interview's goals were cut off with "…").
func TestProjectProposalDialogWrapsLongText(t *testing.T) {
	m, _ := proposalsSurface(t)
	ws := m.d.WS.Root
	m.d.SetProps = setprop.Open(ws)
	long := "v1 accepts many files and directories as arguments and prints a word count for each file plus a grand total, skipping fenced code blocks, front matter and link URLs; the last word is ZEBRA"
	if _, err := m.d.SetProps.Propose(setprop.Proposal{Key: "project.goals", Value: long,
		Layer: setprop.LayerProject, Agent: "atlas", Rationale: "interview answers 2 and 3"}); err != nil {
		t.Fatal(err)
	}
	m.propCur = 0
	feed(m, "v")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "ZEBRA") || strings.Contains(v, "grand total…") {
		t.Fatalf("long proposal clipped:\n%s", v)
	}
}
