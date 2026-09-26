package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/manifest"
	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/ansi"
	dhisettings "github.com/drjzlyan/dhi/internal/settings"
	"github.com/drjzlyan/dhi/internal/testutil/golden"
	"github.com/drjzlyan/dhi/internal/tui/kit"
	"github.com/drjzlyan/dhi/internal/tui/theme"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// agentSurface builds a settings surface wired to a real workspace with
// an org store, so CRUD round-trips through .dhi/agents for real.
func agentSurface(t *testing.T) (*Model, *workspace.Workspace, *org.Org, *int) {
	t.Helper()
	theme.SwapForTest(t, theme.Dark())
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "schema = 1\n\n[members.main]\npath = \"repo\"\n"
	if err := os.WriteFile(filepath.Join(root, workspace.ConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	o, err := org.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	reloads := 0
	m := New(settingsDefaults(), "", Deps{
		WS:     ws,
		Org:    o,
		CLIs:   []string{"claude", "codex"},
		Reload: func() error { reloads++; return nil },
	})
	m.Resize(90, 26)
	// Navigate to the AGENTS section.
	feed(m, "]")
	return m, ws, o, &reloads
}

func settingsDefaults() dhisettings.Config {
	return dhisettings.Defaults()
}

func typeFields(m *Model, vals ...string) {
	for i, v := range vals {
		for m.form.cur() != i {
			feed(m, "tab")
		}
		for _, r := range v {
			m.HandleKey(string(r))
		}
	}
}

func TestAgentsCreateViaForm(t *testing.T) {
	m, ws, _, reloads := agentSurface(t)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "(no agents") {
		t.Fatalf("empty state missing:\n%s", out)
	}

	feed(m, "n")
	if !m.form.open {
		t.Fatal("n did not open the form")
	}
	typeFields(m, "scout", "Scout", "m-1", "You scout.", "", "read, write")
	feed(m, "enter")

	// Round-trip through .dhi/agents (the strict decode is the check).
	roster, err := org.LoadRoster(ws)
	if err != nil || len(roster) != 1 {
		t.Fatalf("roster = %+v err=%v", roster, err)
	}
	a := roster[0]
	if a.ID != "scout" || a.Name != "Scout" || a.Runtime != "claude" ||
		len(a.Tools) != 2 || a.System != "You scout." {
		t.Fatalf("manifest mismatch: %+v", a)
	}
	if *reloads != 1 {
		t.Fatalf("reload seam called %d times, want 1", *reloads)
	}
	if !strings.Contains(ansi.Strip(m.View()), "agent scout saved") {
		t.Fatalf("flash missing:\n%s", ansi.Strip(m.View()))
	}
}

func TestAgentsCreateValidationErrors(t *testing.T) {
	m, _, _, reloads := agentSurface(t)
	feed(m, "n")
	typeFields(m, "scout", "", "", "", "", "") // no name/model
	feed(m, "enter")
	if !m.form.open || m.form.err == "" {
		t.Fatalf("validation did not fail visibly: %+v", m.form)
	}
	if !strings.Contains(m.form.err, "required") {
		t.Fatalf("validation err = %q", m.form.err)
	}
	// The error row renders (panel width may clip long lines — assert a
	// short visible marker plus the state above).
	if !strings.Contains(ansi.Strip(m.View()), "manifest") {
		t.Fatalf("error not rendered:\n%s", ansi.Strip(m.View()))
	}
	if *reloads != 0 {
		t.Fatal("failed form must not reload")
	}
	// Bad tool name is refused with the value named.
	feed(m, "esc", "n")
	typeFields(m, "scout", "Scout", "m-1", "", "", "teleport")
	feed(m, "enter")
	if !strings.Contains(m.form.err, "teleport") {
		t.Fatalf("unknown tool not named: %q", m.form.err)
	}
	// Unknown engine/CLI is named with the offending value (F-030).
	feed(m, "esc", "n")
	typeFields(m, "scout", "Scout", "m-1", "", "", "")
	m.form.f.Fields[4] = kit.NewToggleField("runtime", []string{"nope"}, 0)
	feed(m, "enter")
	if !strings.Contains(m.form.err, "nope") {
		t.Fatalf("bad engine not named: %q", m.form.err)
	}
}

func TestAgentsEditArchiveRestoreDelete(t *testing.T) {
	m, ws, company, reloads := agentSurface(t)
	seed := &manifest.Agent{ID: "scout", Name: "Scout", Model: "m-1",
		Runtime: "claude", Tools: []string{"read"}}
	if err := company.CreateAgent(ws, seed); err != nil {
		t.Fatal(err)
	}

	// Edit: rename + model change; id is immutable.
	feed(m, "e")
	if m.form.orig != "scout" || m.form.f.Fields[1].Value != "Scout" {
		t.Fatalf("edit prefill wrong: %+v", m.form)
	}
	for m.form.cur() != 1 {
		feed(m, "tab")
	}
	for range "Scout v2" {
		m.HandleKey("backspace")
	}
	for _, r := range "Scout v2" {
		m.HandleKey(string(r))
	}
	feed(m, "enter")
	roster, _ := org.LoadRoster(ws)
	if roster[0].Name != "Scout v2" {
		t.Fatalf("edit lost: %+v", roster[0])
	}
	if *reloads != 1 {
		t.Fatalf("reloads = %d, want 1", *reloads)
	}

	// Id immutability is named, not silently ignored.
	feed(m, "e")
	feed(m, "enter") // unchanged id submits fine
	feed(m, "e")
	m.form.f.Fields[0].Value = "other"
	feed(m, "enter")
	if !strings.Contains(m.form.err, "id is immutable") {
		t.Fatalf("immutable id err = %q", m.form.err)
	}
	feed(m, "esc")

	// Archive → restore → archive → delete via the archived route.
	feed(m, "a") // cursor sits on scout (only row)
	if roster, _ := org.LoadRoster(ws); len(roster) != 0 {
		t.Fatal("archive failed")
	}
	feed(m, "a") // restore
	if roster, _ := org.LoadRoster(ws); len(roster) != 1 {
		t.Fatal("restore failed")
	}
	feed(m, "a") // archive again
	feed(m, "x") // delete the archived manifest
	if got := company.Archived(ws); len(got) != 0 {
		t.Fatalf("archived after delete: %v", got)
	}
}

func TestAgentsSectionGolden(t *testing.T) {
	m, ws, company, _ := agentSurface(t)
	seed := &manifest.Agent{ID: "scout", Name: "Scout", Model: "m-1",
		Runtime: "claude", Tools: []string{"read", "write"}}
	if err := company.CreateAgent(ws, seed); err != nil {
		t.Fatal(err)
	}
	seed2 := &manifest.Agent{ID: "muse", Name: "Muse", Model: "m-2",
		Runtime: "codex", Tools: []string{"read"}}
	if err := company.CreateAgent(ws, seed2); err != nil {
		t.Fatal(err)
	}
	if err := company.ArchiveAgent(ws, "muse"); err != nil {
		t.Fatal(err)
	}
	golden.Snapshot(t, "settings_agents", m.View())
}

// company returns the surface's org store (test helper).
func company(m *Model) *org.Org { return m.d.Org }

// mustAgent parses a manifest doc or fails the test.
func mustAgent(t *testing.T, id, doc string) *manifest.Agent {
	t.Helper()
	a, err := manifest.Parse(id, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
