package search

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveRipgrepOnPath proves the production path with the user's rg:
// spawned rg → parsed hits. Skipped when rg is not installed.
func TestLiveRipgrepOnPath(t *testing.T) {
	bin, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("rg is not installed")
	}
	fixture := t.TempDir()
	file := filepath.Join(fixture, "sample.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {\n\tneedleInHaystack()\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, err := Host{Look: func(string) (string, bool) { return bin, true }}.Search(
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
	if _, err := (Host{}).Search(context.Background(), "x", []string{fixture}); err == nil {
		t.Fatal("no rg: the missing-tool error")
	} else if _, ok := err.(*MissingToolError); !ok {
		t.Fatalf("err = %T %v", err, err)
	}
}
