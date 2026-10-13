package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryHealth(t *testing.T) {
	ws := t.TempDir()
	ch := filepath.Join(ws, ".dhi", "channels", "channels")
	if err := os.MkdirAll(ch, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Memory(ws); len(got) != 1 || got[0].Status != OK {
		t.Fatalf("clean = %+v", got)
	}
	_ = os.WriteFile(filepath.Join(ch, "general.jsonl.corrupt"), []byte("x\n"), 0o644)
	_ = os.WriteFile(filepath.Join(ch, "general.jsonl.repair"), []byte("x\n"), 0o644)
	got := Memory(ws)
	if len(got) != 2 || got[0].Status != Fail || got[1].Status != Warn || !strings.Contains(got[1].Detail, "general.jsonl.corrupt") {
		t.Fatalf("checks = %+v", got)
	}
}
