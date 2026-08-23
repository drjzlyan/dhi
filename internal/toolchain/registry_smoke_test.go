package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRegistryPinsEndToEnd exercises the full pipeline against the real
// pinned artifacts (network + ~5MB). Skipped unless DHI_SMOKE_NET=1 so
// ordinary test runs stay hermetic; maintainers run it when touching
// registry pins:
//
//	DHI_SMOKE_NET=1 go test ./internal/toolchain/ -run TestRegistryPinsEndToEnd
func TestRegistryPinsEndToEnd(t *testing.T) {
	if os.Getenv("DHI_SMOKE_NET") != "1" {
		t.Skip("set DHI_SMOKE_NET=1 to verify registry pins against the live artifacts")
	}
	root := t.TempDir()
	m := New(root)
	var sawToolDone bool
	m.OnEvent = func(e Event) {
		if e.Kind == EventToolDone {
			sawToolDone = true
		}
	}

	if err := m.InstallEmbedded(context.Background(), []string{"rg"}); err != nil {
		t.Fatalf("InstallEmbedded(rg): %v", err)
	}
	if !sawToolDone {
		t.Fatal("rg reported no completion event")
	}

	lf, err := m.ReadLockfile()
	if err != nil {
		t.Fatal(err)
	}
	locked, ok := lf.Tools["rg"]
	if !ok || locked.Version == "" {
		t.Fatalf("rg not locked: %+v", lf.Tools)
	}
	if len(locked.SHA256) != 64 {
		t.Errorf("locked digest malformed: %q", locked.SHA256)
	}

	shim := filepath.Join(m.ShimDir(), "rg")
	target, err := os.Readlink(shim)
	if err != nil {
		t.Fatalf("shim missing: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("shim target dangling: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "staging")); len(entries) != 0 {
		t.Error("staging not cleaned")
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "node")); !os.IsNotExist(err) {
		t.Error("unrequested tool installed")
	}
}
