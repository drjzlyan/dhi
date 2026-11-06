package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	// Diffing the snapshots runs git: the dialog opens when it lands.
	if msg := m.openUndoTurn(); !strings.HasPrefix(msg, "checking what @ana") || m.undo != nil {
		t.Fatalf("open: %q (dialog before the diff: %v)", msg, m.undo != nil)
	}
	awaitUndo(t, m)
	v := plainView(m)
	if !strings.Contains(v, "while @ana worked") || !strings.Contains(v, "added.go") {
		t.Fatalf("dialog:\n%s", v)
	}
	m.HandleKey("y")
	if m.undo == nil || !m.undo.restoring || !strings.Contains(plainView(m), "restoring…") {
		t.Fatal("y should start the restore off the UI goroutine")
	}
	awaitUndo(t, m)
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

// A file you saved while the agent worked is kept by y.
func TestUndoTurnKeepsYourOwnSave(t *testing.T) {
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
	before, _ := cps.Snapshot(ctx, "before")
	for _, k := range []string{"A", "!", "esc", ":", "w", "enter"} { // you save during the turn
		m.HandleKey(k)
	}
	after, _ := cps.Snapshot(ctx, "after")
	_ = cps.Record(ctx, checkpoint.Turn{Agent: "ana", Before: before, After: after, At: after.At})
	m.openUndoTurn()
	awaitUndo(t, m)
	if !strings.Contains(plainView(m), "you saved it during the turn") {
		t.Fatalf("dialog:\n%s", plainView(m))
	}
	m.HandleKey("y")
	awaitUndo(t, m)
	if b, _ := os.ReadFile(path); string(b) != "package main!\n" {
		t.Fatalf("your save was reverted: %q", b)
	}
}

// awaitUndo applies messages until undo-turn's diff or restore landed.
func awaitUndo(t *testing.T, m *Model) {
	t.Helper()
	for m.undoLoading || (m.undo != nil && m.undo.restoring) {
		select {
		case msg := <-m.termMsgs:
			m.Update(msg)
		case <-time.After(20 * time.Second):
			t.Fatal("undo turn never finished")
		}
	}
}
