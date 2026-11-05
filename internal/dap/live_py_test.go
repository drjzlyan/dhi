package dap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveDebugpy drives a REAL debugpy (DHI_LIVE_PY=1, python3 with
// debugpy importable): breakpoint, stop with frames/locals, run to exit.
func TestLiveDebugpy(t *testing.T) {
	if os.Getenv("DHI_LIVE_PY") == "" {
		t.Skip("set DHI_LIVE_PY=1 to run against a real debugpy")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	if exec.Command(py, "-c", "import debugpy.adapter").Run() != nil {
		t.Skip("python3 cannot import debugpy")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.py")
	if err := os.WriteFile(src, []byte("def square(n):\n    r = n * n\n    return r\n\n\nprint(square(7))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := StartDebugpy(ctx, dir, os.Environ())
	if err != nil {
		t.Fatalf("StartDebugpy: %v", err)
	}
	s, err := Start(ctx, c, pythonLaunch(src, dir, ""), map[string][]int{src: {3}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(ctx)
	st := waitState(t, s, func(st State) bool { return st.Stopped || st.Exited }, "first stop")
	if !st.Stopped || len(st.Frames) == 0 {
		t.Fatalf("expected a breakpoint stop, got %+v", st)
	}
	f := st.Frames[0]
	if !strings.HasSuffix(f.Path, "main.py") || f.Line != 3 || f.Name != "square" {
		t.Fatalf("wrong stop: %+v", f)
	}
	var sawR bool
	for _, v := range st.Locals {
		if v.Name == "r" && v.Value == "49" {
			sawR = true
		}
	}
	if !sawR {
		t.Fatalf("expected local r=49, got %+v", st.Locals)
	}
	if err := s.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, func(st State) bool { return st.Exited }, "exit")
}
