package settings

import (
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/forge"
	"github.com/drjzlyan/dhi/internal/ansi"
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
	for _, want := range []string{"PENDING", "race_test", "FORGED TOOLS", "custom__check_all", "0 call(s)"} {
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
