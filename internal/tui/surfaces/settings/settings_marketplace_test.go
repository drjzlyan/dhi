package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/agentkit/registry"
)

// marketplaceFixture writes a valid pack and an index pinning its digest.
func marketplaceFixture(t *testing.T) string {
	t.Helper()
	packDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(packDir, "scout.toml"),
		[]byte("schema = 1\nname = \"Scout\"\nmodel = \"m\"\ntools = [\"read\"]\nruntime = \"claude\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "pack.toml"),
		[]byte("schema = 2\nname = \"ux\"\nversion = \"1.0.0\"\ndescription = \"a UX crew\"\nagents = [\"scout.toml\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := registry.Digest(packDir)
	if err != nil {
		t.Fatal(err)
	}
	indexDir := t.TempDir()
	body := "schema = 1\n[[pack]]\nname = \"ux\"\nversion = \"1.0.0\"\ndescription = \"a UX crew\"\n" +
		"source = \"" + packDir + "\"\nsha256 = \"" + sum + "\"\n"
	if err := os.WriteFile(filepath.Join(indexDir, "index.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return indexDir
}

func gotoMarketplace(m *Model) {
	for m.sec != secMarketplace {
		feed(m, "]")
	}
}

func TestMarketplaceRefreshBrowseInstall(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	indexDir := marketplaceFixture(t)
	m.d.Registry = registry.New(ws)
	gotoMarketplace(m)

	// No cache yet: a named hint, then a refresh sets the source.
	if view := strings.Join(m.marketplaceView(80), "\n"); !strings.Contains(view, "no registry") {
		t.Fatalf("empty view = %q", view)
	}
	feed(m, "r")
	if m.dlg == nil || m.dkind != dlgRegistrySource {
		t.Fatal("r did not open the registry source dialog")
	}
	typeDialog(m, indexDir)
	feed(m, "enter")
	drainDialogEvent(t, m)
	if !strings.Contains(m.flash, "registry refreshed") {
		t.Fatalf("refresh flash = %q", m.flash)
	}
	view := strings.Join(m.marketplaceView(80), "\n")
	if !strings.Contains(view, "ux") || !strings.Contains(view, "1.0.0") {
		t.Fatalf("browse view = %q", view)
	}

	// Install via confirm → digest verified → agent lands.
	feed(m, "enter")
	if m.dkind != dlgRegistryInstall {
		t.Fatal("enter did not open the install confirm")
	}
	feed(m, "enter")
	drainDialogEvent(t, m)
	if !strings.Contains(m.flash, "installed pack ux") {
		t.Fatalf("install flash = %q", m.flash)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, ".dhi", "agents", "scout.toml")); err != nil {
		t.Fatalf("agent not installed: %v", err)
	}
}

func TestMarketplaceSearchAndInspect(t *testing.T) {
	m, ws, _, _ := agentSurface(t)
	indexDir := marketplaceFixture(t)
	reg := registry.New(ws)
	if err := reg.Refresh(context.Background(), indexDir); err != nil {
		t.Fatal(err)
	}
	m.d.Registry = reg
	gotoMarketplace(m)

	// Search filters.
	feed(m, "f")
	for _, r := range "zzz" {
		feed(m, string(r))
	}
	if view := strings.Join(m.marketplaceView(80), "\n"); !strings.Contains(view, "no packs match") {
		t.Fatalf("filtered view = %q", view)
	}
	feed(m, "esc")
	feed(m, "backspace") // still editing off? esc leaves edit; query remains "zzz"
	m.mktQuery = ""
	// Inspect shows the full digest.
	feed(m, "v")
	if m.dlg == nil || m.dkind != dlgDisplay {
		t.Fatal("v did not open the inspect dialog")
	}
	joined := strings.Join(m.dlg.Lines, "\n")
	if !strings.Contains(joined, "sha256") {
		t.Fatalf("inspect = %q", joined)
	}
}

func TestMarketplaceUnavailable(t *testing.T) {
	m, _, _, _ := agentSurface(t)
	gotoMarketplace(m)
	if view := strings.Join(m.marketplaceView(80), "\n"); !strings.Contains(view, "unavailable") {
		t.Fatalf("nil registry view = %q", view)
	}
	// Keys with a nil registry must not panic.
	feed(m, "j")
	feed(m, "enter")
	feed(m, "r")
}
