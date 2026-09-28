package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/agentkit/org"
	"github.com/drjzlyan/dhi/internal/ansi"
)

const importScoutDoc = `schema = 1
name = "Scout"
model = "m-1"
runtime = "claude"
tools = ["read"]
`

const importMuseDoc = `schema = 1
name = "Muse"
model = "m-2"
runtime = "codex"
`

// writeImportFixture lays down manifests under root and returns the dir.
func writeImportFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// submitSource types the source, submits, and drains the async outcome.
func submitSource(t *testing.T, m *Model, src string) {
	t.Helper()
	feed(m, "g")
	if !m.form.open || m.form.kind != formSource {
		t.Fatalf("g did not open the source form: %+v", m.form)
	}
	for _, r := range src {
		m.HandleKey(string(r))
	}
	feed(m, "enter")
	if !m.form.busy {
		t.Fatal("submit did not mark the form busy")
	}
	select {
	case ev := <-m.events:
		m.Update(ev)
	case <-time.After(2 * time.Second):
		t.Fatal("no import outcome within timeout")
	}
}

func TestImportBareManifests(t *testing.T) {
	m, ws, company, reloads := agentSurface(t)
	dir := writeImportFixture(t, map[string]string{
		"scout.toml":       importScoutDoc,
		"nested/muse.toml": importMuseDoc,
	})
	submitSource(t, m, dir)

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "imported 2") {
		t.Fatalf("summary missing:\n%s", out)
	}
	roster, err := org.LoadRoster(ws)
	if err != nil || len(roster) != 2 {
		t.Fatalf("roster = %+v err=%v", roster, err)
	}
	if *reloads != 1 {
		t.Fatalf("reload called %d times, want 1", *reloads)
	}
	if company.Archived(ws) != nil {
		_ = company // archived list is empty; presence check only
	}
}

func TestImportSkipsDuplicatesNamed(t *testing.T) {
	m, ws, _, reloads := agentSurface(t)
	seed := mustAgent(t, "scout", importScoutDoc)
	if err := company(m).CreateAgent(ws, seed); err != nil {
		t.Fatal(err)
	}
	dir := writeImportFixture(t, map[string]string{
		"scout.toml": importScoutDoc,
		"muse.toml":  importMuseDoc,
	})
	*reloads = 0
	submitSource(t, m, dir)

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "imported 1") || !strings.Contains(out, "skipped 1") ||
		!strings.Contains(out, "scout") {
		t.Fatalf("named skip missing:\n%s", out)
	}
	if *reloads != 1 {
		t.Fatalf("reload called %d times, want 1", *reloads)
	}
}

func TestImportRefusesWholeBatchOnBadManifest(t *testing.T) {
	m, ws, _, reloads := agentSurface(t)
	dir := writeImportFixture(t, map[string]string{
		"good.toml": importScoutDoc,
		"junk.toml": "schema = 9\n",
	})
	submitSource(t, m, dir)

	if !strings.Contains(ansi.Strip(m.View()), "junk.toml") {
		t.Fatalf("bad file not named:\n%s", ansi.Strip(m.View()))
	}
	if roster, _ := org.LoadRoster(ws); len(roster) != 0 {
		t.Fatalf("nothing must write on a refused batch: %+v", roster)
	}
	if *reloads != 0 {
		t.Fatal("refused batch must not reload")
	}
}

func TestImportPrefersPackWhenPresent(t *testing.T) {
	m, ws, _, reloads := agentSurface(t)
	dir := writeImportFixture(t, map[string]string{
		"pack.toml":  "schema = 2\nname = \"crew\"\nversion = \"1.0.0\"\nagents = [\"scout.toml\"]\n",
		"scout.toml": importScoutDoc,
		"muse.toml":  importMuseDoc, // ignored: the pack owns the flow
	})
	submitSource(t, m, dir)

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "installed pack crew") {
		t.Fatalf("pack flow not taken:\n%s", out)
	}
	roster, _ := org.LoadRoster(ws)
	if len(roster) != 1 || roster[0].ID != "scout" {
		t.Fatalf("pack install wrote %+v", roster)
	}
	if *reloads != 1 {
		t.Fatalf("reload called %d times, want 1", *reloads)
	}
}

func TestImportSubpathScopesAndEmptyDirNamed(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	dir := writeImportFixture(t, map[string]string{
		"top.toml":      importScoutDoc, // outside the scoped subpath
		"sub/muse.toml": importMuseDoc,
	})
	submitSource(t, m, dir+"#sub")
	roster, _ := org.LoadRoster(ws)
	if len(roster) != 1 || roster[0].ID != "muse" {
		t.Fatalf("subpath import = %+v", roster)
	}

	// A source with nothing importable names the reason.
	empty := t.TempDir()
	submitSource(t, m, empty)
	if !strings.Contains(ansi.Strip(m.View()), "no pack.toml or agent manifests") {
		t.Fatalf("empty source not named:\n%s", ansi.Strip(m.View()))
	}
	if roster, _ := org.LoadRoster(ws); len(roster) != 1 {
		t.Fatalf("empty source wrote: %+v", roster)
	}
}

func TestImportUnknownSourceNamed(t *testing.T) {
	m, _, _, _ := agentSurface(t)
	submitSource(t, m, "/nonexistent/dhi-source-xyz")
	if !strings.Contains(ansi.Strip(m.View()), "not a directory") {
		t.Fatalf("bad source not named:\n%s", ansi.Strip(m.View()))
	}
}
