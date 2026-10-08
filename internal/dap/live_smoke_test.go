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

// TestLiveDelve drives a REAL delve through the whole client: handshake,
// breakpoint, stop with frames/locals, step, evaluate, run to exit.
// Gate-style like DHI_SMOKE_NET / DHI_SMOKE_GIT:
//
//	DHI_SMOKE_DAP=<dir containing dlv> DHI_SMOKE_DAP_PROG=<dir with main.go+go.mod> \
//	  go test ./internal/dap/ -run TestLiveDelve -v
//
// The program's main.go must have `r := n * n` on line 6 inside square().
func TestLiveDelve(t *testing.T) {
	dlvDir, prog := os.Getenv("DHI_SMOKE_DAP"), os.Getenv("DHI_SMOKE_DAP_PROG")
	if dlvDir == "" || prog == "" {
		t.Skip("set DHI_SMOKE_DAP (dir with dlv) and DHI_SMOKE_DAP_PROG to run against real delve")
	}
	// StartDelve resolves dlv from the supplied env's PATH only; delve then
	// shells out to `go build`, so the Go toolchain's dir rides along.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH to build the target program")
	}
	env := filterPath(os.Environ(), dlvDir, filepath.Dir(goBin))
	old := launchTimeout
	launchTimeout = 30 * time.Second
	defer func() { launchTimeout = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c, err := StartDelve(ctx, prog, env)
	if err != nil {
		t.Fatalf("StartDelve: %v", err)
	}
	src := filepath.Join(prog, "main.go")
	s, err := Start(ctx, c, LaunchConfig{Mode: "debug", Program: prog, Cwd: prog}, map[string][]int{src: {6}})
	if err != nil {
		if strings.Contains(err.Error(), "never sent the initialized") {
			// macOS's lldb debugserver path blocks until Developer mode /
			// developer-tools authorization is granted (`DevToolsSecurity
			// -status`); nothing the client can do.
			t.Skipf("delve did not finish launching (macOS Developer mode off?): %v", err)
		}
		if strings.Contains(err.Error(), "native backend") {
			t.Skipf("delve cannot launch here (the adapter said so, promptly): %v", err)
		}
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(ctx)

	st := waitState(t, s, func(st State) bool { return st.Stopped || st.Exited }, "first stop")
	if !st.Stopped || len(st.Frames) == 0 {
		t.Fatalf("expected a breakpoint stop, got %+v", st)
	}
	f := st.Frames[0]
	t.Logf("stopped: reason=%s frame=%s %s:%d locals=%+v", st.Reason, f.Name, f.Path, f.Line, st.Locals)
	if !strings.HasSuffix(f.Path, "main.go") || f.Line != 6 || !strings.Contains(f.Name, "square") {
		t.Fatalf("wrong stop location: %+v", f)
	}
	var sawN bool
	for _, v := range st.Locals {
		if v.Name == "n" && v.Value == "7" {
			sawN = true
		}
	}
	if !sawN {
		t.Fatalf("expected local n=7, got %+v", st.Locals)
	}

	out, err := s.Evaluate(ctx, "n*3")
	if err != nil || out != "21" {
		t.Fatalf("evaluate n*3 = %q err=%v", out, err)
	}
	if err := s.Next(ctx); err != nil {
		t.Fatal(err)
	}
	st = waitState(t, s, func(st State) bool { return st.Stopped && st.StopSeq == 2 }, "step stop")
	if st.Frames[0].Line != 7 {
		t.Fatalf("after next expected line 7, got %+v", st.Frames[0])
	}
	if err := s.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	st = waitState(t, s, func(st State) bool { return st.Exited }, "exit")
	t.Logf("exited code=%d output=%q", st.ExitCode, st.Output)
	if !strings.Contains(strings.Join(st.Output, "\n"), "result 49") {
		t.Fatalf("program output not captured: %q", st.Output)
	}
}

// filterPath makes dlvDir the ONLY PATH entry plus the system dirs delve
// needs, so StartDelve's env-only lookup is what finds dlv.
func filterPath(env []string, dlvDir, goDir string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+dlvDir+":"+goDir+":/usr/bin:/bin")
}
