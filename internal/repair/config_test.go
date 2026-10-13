package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-090: values an older DHI copied from your config into the workspace's
// are found by name and removed; the workspace's own choices stay.
func TestConfigCopiesRemovesOnlyTheCopies(t *testing.T) {
	in := fixture(t)
	in.UserConfig = filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(in.UserConfig, []byte("schema = 1\ntheme = \"light-paper\"\nengine = \"cli:claude\"\n"), 0o644)
	ws := filepath.Join(in.WS.Root, ".dhi", "config.toml")
	_ = os.WriteFile(ws, []byte("schema = 1\ntheme = \"light-paper\"\nengine = \"cli:codex\"\n"), 0o644)
	r, ok := byID(Find(in), "config-copies")
	if !ok || !r.Safe || !strings.Contains(r.What, "theme") || strings.Contains(r.What, "engine") {
		t.Fatalf("repair = %+v ok=%v", r, ok)
	}
	if err := r.Apply(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ws)
	if strings.Contains(string(b), "theme") || !strings.Contains(string(b), "cli:codex") {
		t.Fatalf("workspace config:\n%s", b)
	}
	if _, ok := byID(Find(in), "config-copies"); ok {
		t.Fatal("offered again after the fix")
	}
}
