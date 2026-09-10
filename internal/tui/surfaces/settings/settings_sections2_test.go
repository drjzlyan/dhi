package settings

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/bus"
	"github.com/drjzlyan/dhi/internal/agentkit/standards"
	"github.com/drjzlyan/dhi/internal/ansi"
	"github.com/drjzlyan/dhi/internal/autopilot"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// ---- STANDARDS ----

func TestStandardsLayersAndPreview(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.sec = secStandards
	writeRosterAgent(t, ws.Root, "scout")

	// workspace layer via w
	feed(m, "w")
	typeDialog(m, "no-tabs, indent-4")
	feed(m, "enter")
	snap, err := standards.Inspect(ws.Root)
	if err != nil || len(snap.Workspace) != 2 {
		t.Fatalf("workspace layer not saved: %+v err=%v", snap, err)
	}

	// agent layer via g (rows: workspace, scout)
	feed(m, "j")
	feed(m, "g")
	if m.dform == nil || m.dtarget != "@agent:scout" {
		t.Fatalf("g did not open the agent layer dialog: %+v %q", m.dlg, m.dtarget)
	}
	if got := m.dform.Values()[0]; got != "scout" {
		t.Fatalf("agent id not prefilled: %q", got)
	}
	typeDialog(m, "", "", "no-any")
	feed(m, "enter")
	snap, err = standards.Inspect(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	ov, ok := snap.Agents["scout"]
	if !ok || len(ov.Entries) != 1 || ov.Entries[0] != "no-any" {
		t.Fatalf("agent override missing: %+v", snap.Agents)
	}
	if ov.Mode != standards.ModeExtend {
		t.Fatalf("mode = %q", ov.Mode)
	}

	// preview via v
	feed(m, "v")
	typeDialog(m, "scout")
	feed(m, "enter")
	if m.dform != nil || m.dlg == nil || m.dkind != dlgDisplay {
		t.Fatalf("preview did not open a display modal: %+v", m.dlg)
	}
	if !strings.Contains(ansi.Strip(m.View()), "no-tabs") {
		t.Fatalf("preview block missing:\n%s", ansi.Strip(m.View()))
	}
	feed(m, "esc")
	if m.dlg != nil {
		t.Fatal("esc did not close the display modal")
	}
}

func TestStandardsSectionGolden(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	m.sec = secStandards
	writeRosterAgent(t, ws.Root, "scout")
	if err := standards.Save(ws.Root, []string{"no-tabs"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	golden.Snapshot(t, "settings_standards", m.View())
}

// ---- AUTOPILOTS ----

type fakeTurn struct{ handled chan bus.Message }

func (f *fakeTurn) Handle(_ context.Context, msg bus.Message) { f.handled <- msg }

func wireAutopilots(t *testing.T, m *Model, ws *workspace.Workspace) (*bus.Bus, *fakeTurn) {
	t.Helper()
	b, err := bus.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	ft := &fakeTurn{handled: make(chan bus.Message, 4)}
	m.d.Bus = b
	m.d.Runtime = ft
	return b, ft
}

func TestAutopilotCreateRunNowAndRemove(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	writeRosterAgent(t, ws.Root, "scout")
	m.d.Autopilots = mustAutoStore(t, ws)
	m.sec = secAutopilots
	b, ft := wireAutopilots(t, m, ws)

	// create via dialog
	feed(m, "n")
	if m.dform == nil {
		t.Fatal("n did not open the dialog")
	}
	typeDialog(m, "sweep", "Sweep", "scout", "check the inbox", "")
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("dialog did not close: %+v", m.dform.Err)
	}
	cards := m.d.Autopilots.List()
	if len(cards) != 1 || cards[0].Slug != "sweep" || !cards[0].Enabled {
		t.Fatalf("cards = %+v", cards)
	}

	// run now: post to the DM + dispatch + MarkRan
	feed(m, "r")
	select {
	case msg := <-ft.handled:
		if msg.Channel != "dm:scout" || msg.Author != bus.Human {
			t.Fatalf("dispatch = %+v", msg)
		}
		if !strings.Contains(msg.Text, "[autopilot sweep]") {
			t.Fatalf("prompt tag missing: %q", msg.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime never dispatched the run")
	}
	if len(b.History("dm:scout", 0)) != 1 {
		t.Fatal("dm post missing")
	}
	card, _ := m.d.Autopilots.Get("sweep")
	if card.LastRun.IsZero() {
		t.Fatal("MarkRan not recorded on success")
	}

	// dangling agent refuses by name and does not mark ran. Cards list
	// in slug order, so the new ghost card is already under the cursor.
	feed(m, "n")
	typeDialog(m, "ghost", "Ghost", "nobody", "x", "")
	feed(m, "enter")
	if m.dlg != nil {
		t.Fatalf("ghost card not created: %+v", m.dform.Err)
	}
	feed(m, "r")
	if !strings.Contains(m.flash, "not on roster") {
		t.Fatalf("dangling refusal missing: %q", m.flash)
	}
	ghost, _ := m.d.Autopilots.Get("ghost")
	if !ghost.LastRun.IsZero() {
		t.Fatal("dangling card must not be marked ran")
	}

	// arm/pause: cursor moves to sweep (slug order puts sweep second)
	feed(m, "j")
	feed(m, "e")
	if card, _ := m.d.Autopilots.Get("sweep"); card.Enabled {
		t.Fatal("e did not pause the card")
	}

	// remove via confirm
	feed(m, "x")
	feed(m, "enter")
	if rows := m.d.Autopilots.List(); len(rows) != 1 {
		t.Fatalf("cards after remove = %+v", rows)
	}
}

func TestAutopilotsSectionGolden(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	writeRosterAgent(t, ws.Root, "scout")
	m.d.Autopilots = mustAutoStore(t, ws)
	m.d.Runtime = &fakeTurn{handled: make(chan bus.Message, 4)}
	m.sec = secAutopilots
	sch, _ := autopilot.ParseSchedule("daily 09:00")
	if _, err := m.d.Autopilots.Create("sweep", "Sweep", "scout", "check the inbox", sch); err != nil {
		t.Fatal(err)
	}
	golden.Snapshot(t, "settings_autopilots", m.View())
}

// ---- AGENTS profile (the INSPECT re-host) ----

func TestAgentsProfileModal(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	writeRosterAgent(t, ws.Root, "scout")

	feed(m, "v")
	if m.dform != nil || m.dlg == nil || m.dkind != dlgDisplay {
		t.Fatalf("v did not open the profile modal: %+v", m.dlg)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "model m-1") || !strings.Contains(out, "on roster") {
		t.Fatalf("profile content missing:\n%s", out)
	}
	feed(m, "esc")
	if m.dlg != nil {
		t.Fatal("esc did not close the profile modal")
	}
}
