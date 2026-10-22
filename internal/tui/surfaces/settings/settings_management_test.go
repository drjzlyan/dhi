package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/library"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// mustAutoStore opens the autopilot store for the fixture workspace.
func mustAutoStore(t *testing.T, ws *workspace.Workspace) *autopilot.Store {
	t.Helper()
	s, err := autopilot.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// typeDialog types values into the open kit.Form dialog, tabbing
// between fields (the dialog twin of typeFields).
func typeDialog(m *Model, vals ...string) {
	for i, v := range vals {
		for m.dform.Cur() != i {
			feed(m, "tab")
		}
		for _, r := range v {
			m.HandleKey(string(r))
		}
	}
}

// drainDialogEvent waits for one async dialog outcome.
func drainDialogEvent(t *testing.T, m *Model) {
	t.Helper()
	select {
	case ev := <-m.events:
		m.Update(ev)
	case <-time.After(2 * time.Second):
		t.Fatal("no dialog outcome within timeout")
	}
}

// writeRosterAgent drops one valid manifest into .dhi/agents.
func writeRosterAgent(t *testing.T, wsRoot, id string) {
	t.Helper()
	dir := filepath.Join(wsRoot, ".dhi", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf("schema = 1\nname = %q\nmodel = \"m-1\"\nruntime = \"claude\"\n", id)
	if err := os.WriteFile(filepath.Join(dir, id+".toml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTeamsCreateEditDeleteRoundTrip(t *testing.T) {
	m, ws, o, _ := agentSurface(t)
	m.sec = secTeams
	writeRosterAgent(t, ws.Root, "scout")

	feed(m, "n")
	if m.dlg == nil || m.dform == nil || m.dkind != dlgTeam {
		t.Fatal("n did not open the team dialog")
	}
	typeDialog(m, "platform", "", "scout, fixer")
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("dialog did not close on submit: %+v", m.dform.Err)
	}
	teams := o.Teams()
	if len(teams) != 1 || teams[0].Name != "platform" ||
		teams[0].Lead != "you" || len(teams[0].Members) != 2 {
		t.Fatalf("team not created: %+v", teams)
	}

	// edit: cycle the lead to scout (roster choice)
	feed(m, "e")
	if m.dform == nil || m.dform.Values()[0] != "platform" {
		t.Fatalf("edit did not prefill: %+v", m.dform)
	}
	feed(m, "tab")   // name → lead
	feed(m, "right") // you → scout
	feed(m, "enter")
	teams = o.Teams()
	if teams[0].Lead != "scout" {
		t.Fatalf("lead not updated: %+v", teams[0])
	}

	// delete via confirm
	feed(m, "x")
	if m.dlg == nil || m.dform != nil || m.dkind != dlgTeamDelete {
		t.Fatal("x did not open the confirm dialog")
	}
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatal("confirm did not close")
	}
	if len(o.Teams()) != 0 {
		t.Fatalf("team not deleted: %+v", o.Teams())
	}
	if !strings.Contains(ansi.Strip(fullView(m)), "team platform deleted") {
		t.Fatal("flash missing")
	}
}

func TestTeamsValidationRefusesEmptyName(t *testing.T) {
	m, _, o, _ := agentSurface(t)
	m.sec = secTeams
	feed(m, "n")
	feed(m, "enter") // name empty
	if m.dlg == nil || m.dform.Err == "" {
		t.Fatal("empty name accepted")
	}
	if !strings.Contains(m.dform.Err, "required") {
		t.Fatalf("err = %q", m.dform.Err)
	}
	if len(o.Teams()) != 0 {
		t.Fatal("refused form must not write")
	}
}

func TestTeamsSectionGolden(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.sec = secTeams
	writeRosterAgent(t, ws.Root, "scout")
	if err := m.d.Org.CreateTeam("platform", "scout", []string{"scout"}); err != nil {
		t.Fatal(err)
	}
	golden.Snapshot(t, "settings_teams", m.View())
}

func TestTeamsAddByRoleCreatesOneTeamPerLead(t *testing.T) {
	m, ws, o, reloads := agentSurface(t)
	m.d.Library = library.Open(ws)
	m.sec = secTeams
	feed(m, "a")
	if m.dkind != dlgTeamCompose || m.dform == nil {
		t.Fatal("a did not open the compose dialog")
	}
	feed(m, "tab", "right", "right")          // leads 2
	feed(m, "tab", "right", "right", "right") // fullstack 3
	golden.Snapshot(t, "settings_teams_compose", m.View())
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("dialog stayed open: %s", m.dform.Err)
	}
	if len(o.Teams()) != 2 || *reloads != 1 {
		t.Fatalf("teams %+v reloads %d", o.Teams(), *reloads)
	}
	roster, _ := org.LoadRoster(ws)
	if len(roster) != 5 {
		t.Fatalf("%d agents", len(roster))
	}
	if !strings.Contains(ansi.Strip(fullView(m)), "added atlas") {
		t.Fatalf("flash missing:\n%s", ansi.Strip(fullView(m)))
	}
}

func TestTeamsAddByRoleIntoExistingTeam(t *testing.T) {
	m, ws, o, _ := agentSurface(t)
	m.d.Library = library.Open(ws)
	writeRosterAgent(t, ws.Root, "atlas")
	if err := o.CreateTeam("core", "atlas", []string{"atlas"}); err != nil {
		t.Fatal(err)
	}
	m.sec = secTeams
	feed(m, "a", "right") // into: core
	for i := 0; i < 5; i++ {
		feed(m, "tab")
	}
	feed(m, "right") // testers 1
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("dialog stayed open: %s", m.dform.Err)
	}
	tm, _ := o.Team("core")
	if len(tm.Members) != 2 || tm.Lead != "atlas" {
		t.Fatalf("core = %+v", tm)
	}
	for _, id := range tm.Members {
		if id == "atlas" {
			continue
		}
		if id != "forge" {
			t.Fatalf("new member %q (atlas is taken, forge is next)", id)
		}
	}
}

func TestTeamsAddByRoleRefusesNothingAndLeadsIntoATeam(t *testing.T) {
	m, ws, o, _ := agentSurface(t)
	m.d.Library = library.Open(ws)
	m.sec = secTeams
	feed(m, "a", "enter")
	if m.dlg == nil || !strings.Contains(m.dform.Err, "at least one") {
		t.Fatalf("empty compose accepted: %+v", m.dform)
	}
	feed(m, "esc")
	_ = o.CreateTeam("core", "you", nil)
	feed(m, "a", "right", "tab", "right", "enter") // into core, leads 1
	if m.dlg == nil || !strings.Contains(m.dform.Err, "leads start their own team") {
		t.Fatalf("lead into a team accepted: %+v", m.dform)
	}
}

func TestTeamsAddByRoleFollowsHowYouRead(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.d.Library = library.Open(ws)
	m.cfg.Profile.Reading.Detail = "brief"
	m.sec = secTeams
	feed(m, "a", "tab", "right", "enter") // one lead
	roster, _ := org.LoadRoster(ws)
	if len(roster) != 1 || roster[0].Persona != "terse" {
		t.Fatalf("roster %+v", roster[0])
	}
}
