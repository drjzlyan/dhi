package editor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/dap"
	"github.com/drjzlyan/dhi/internal/textbuf"
	"github.com/drjzlyan/dhi/internal/workspace"
)

// TestLiveDebugThroughTheEditor drives :break/:debug/:next/:eval/:cont
// against a REAL delve (the same flow as dap.TestLiveDelve, but through
// the editor's commands, panel and gutter). Gated:
//
//	DHI_SMOKE_DAP=<dir with dlv> DHI_SMOKE_DAP_PROG=<dir with go.mod+main.go> \
//	  go test ./internal/tui/surfaces/editor -run TestLiveDebugThroughTheEditor -v
//
// main.go must have `r := n * n` on line 6 inside square().
func TestLiveDebugThroughTheEditor(t *testing.T) {
	dlvDir, prog := os.Getenv("DHI_SMOKE_DAP"), os.Getenv("DHI_SMOKE_DAP_PROG")
	if dlvDir == "" || prog == "" {
		t.Skip("set DHI_SMOKE_DAP and DHI_SMOKE_DAP_PROG to debug a real program with real delve")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH")
	}
	env := []string{"HOME=" + os.Getenv("HOME"), "PATH=" + dlvDir + ":" + filepath.Dir(goBin) + ":/usr/bin:/bin"}

	root := t.TempDir()
	cfg := "schema = 1\n\n[members.prog]\npath = \"" + prog + "\"\n"
	if err := os.MkdirAll(filepath.Join(root, ".dhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dhi", "workspace.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := New("test", ws, WithTermEnv(env))
	m.Resize(130, 40)
	m.dapStart = func(ctx context.Context, dir string, e []string) (*dap.Client, error) {
		return dap.StartDelve(ctx, dir, e)
	}
	src := filepath.Join(prog, "main.go")
	if n := m.OpenPaths(src); n != 1 {
		t.Fatalf("OpenPaths opened %d", n)
	}
	m.active().Buffer().SetCursor(textbuf.Pos{Line: 5}) // line 6 (1-based)
	runEx(m, "break")
	if bl := m.breakpoints[src]; len(bl) != 1 || bl[0] != 6 {
		t.Fatalf("breakpoints = %v", m.breakpoints)
	}

	runEx(m, "debug")
	waitForLong(t, m, func() bool { return m.mode == modeDebug }, "real breakpoint stop")
	out := plainView(m)
	for _, want := range []string{"debugger — breakpoint", "main.square", "main.go:6"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q:\n%s", want, out)
		}
	}
	if st := m.dbg.st; len(st.Locals) == 0 || st.Locals[0].Name != "n" || st.Locals[0].Value != "7" {
		t.Fatalf("locals = %+v", st.Locals)
	}
	state, _ := m.DebugState()
	if !strings.Contains(state, "stopped: breakpoint") || !strings.Contains(state, "n") {
		t.Fatalf("DebugState = %q", state)
	}

	feed(m, "esc")
	if !strings.Contains(plainView(m), "6▶") {
		t.Fatalf("stop-line marker missing:\n%s", plainView(m))
	}
	runEx(m, "eval n*3")
	waitForLong(t, m, func() bool { return strings.Contains(m.active().Message(), "n*3 = 21") }, "eval result")

	runEx(m, "next")
	waitForLong(t, m, func() bool { return m.dbg != nil && m.dbg.st.StopSeq == 2 && m.mode == modeDebug }, "step stop")
	if f := m.dbg.st.Frames[0]; f.Line != 7 {
		t.Fatalf("after next: %+v", f)
	}
	feed(m, "esc")
	runEx(m, "cont")
	waitForLong(t, m, func() bool { return m.dbg == nil }, "program exit")
	if got := m.active().Message(); !strings.Contains(got, "program exited (code 0)") {
		t.Fatalf("exit message = %q", got)
	}
}

// waitForLong is waitFor with a deadline fit for a real compile + launch.
func waitForLong(t *testing.T, m *Model, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		m.drainTerm()
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s:\n%s", what, plainView(m))
}
