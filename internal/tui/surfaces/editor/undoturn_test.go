package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/checkpoint"
	"github.com/drjzlyan/dhi/internal/gitcore"
)

// F-120: an agent's chat turn is undone from the editor; a file you edited
// since is kept unless you say f.
func TestUndoAgentTurn(t *testing.T) {
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	m, path := openApp(t)
	alpha := filepath.Dir(path)
	cps := checkpoint.Open(m.ws.Root, gitcore.NewRunner(bin, os.Environ()), func() []checkpoint.Member {
		return []checkpoint.Member{{Name: "alpha", Path: alpha}}
	})
	m.cps = cps
	ctx := context.Background()
	before, err := cps.Snapshot(ctx, "before")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(path, []byte("package agent\n"), 0o644)
	_ = os.WriteFile(filepath.Join(alpha, "added.go"), []byte("package x\n"), 0o644)
	after, _ := cps.Snapshot(ctx, "after")
	_ = cps.Record(ctx, checkpoint.Turn{Agent: "ana", Before: before, After: after, At: after.At})

	if msg := m.openUndoTurn(); msg != "" {
		t.Fatal(msg)
	}
	v := plainView(m)
	if !strings.Contains(v, "@ana's turn") || !strings.Contains(v, "added.go") {
		t.Fatalf("dialog:\n%s", v)
	}
	m.HandleKey("y")
	if b, _ := os.ReadFile(path); string(b) != "package main\n" {
		t.Fatalf("app.go = %q", b)
	}
	if _, err := os.Stat(filepath.Join(alpha, "added.go")); !os.IsNotExist(err) {
		t.Fatal("added file kept")
	}
	if m.active().Buffer().Text() != "package main\n" {
		t.Fatal("open buffer did not follow")
	}
	if len(cps.Turns()) != 0 {
		t.Fatal("an undone turn stays listed")
	}
}
