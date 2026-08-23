package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/toolchain"
)

// TestLiveRipgrepThroughToolchainShim proves the full production path:
// pinned artifact → verified install → shim symlink → spawned rg →
// parsed hits. Gated like the other network smokes:
//
//	DHI_SMOKE_NET=1 go test ./internal/search/ -run TestLiveRipgrep
func TestLiveRipgrepThroughToolchainShim(t *testing.T) {
	if os.Getenv("DHI_SMOKE_NET") != "1" {
		t.Skip("set DHI_SMOKE_NET=1 to verify the live rg pipeline")
	}
	root := t.TempDir()
	m := toolchain.New(root)
	if err := m.InstallEmbedded(context.Background(), []string{"rg"}); err != nil {
		t.Fatalf("install rg: %v", err)
	}

	fixture := t.TempDir()
	file := filepath.Join(fixture, "sample.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {\n\tneedleInHaystack()\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, err := Ripgrep{Bin: filepath.Join(m.ShimDir(), "rg")}.Search(
		context.Background(), "needleinhaystack", []string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for h := range hits {
		if h.Path == file && strings.Contains(h.Text, "needleInHaystack") {
			found = true
		}
	}
	if !found {
		t.Fatal("live search missed the fixture hit")
	}
}
