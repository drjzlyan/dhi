package dap

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/testutil/dapfake"
	"github.com/drjzlyan/dhi/internal/toolchain"
)

func newFake(t *testing.T) (*Client, *dapfake.Adapter) {
	t.Helper()
	conn, f := dapfake.New(t)
	c := New(conn)
	t.Cleanup(func() { _ = c.Close() })
	return c, f
}

func waitState(t *testing.T, s *Session, cond func(State) bool, what string) State {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if st := s.State(); cond(st) {
			return st
		}
		select {
		case <-s.Updates():
		case <-deadline:
			t.Fatalf("timeout waiting for %s; state=%+v", what, s.State())
		}
	}
}

func start(t *testing.T) (*Session, *dapfake.Adapter) {
	t.Helper()
	c, f := newFake(t)
	s, err := Start(context.Background(), c, LaunchConfig{Mode: "debug", Program: "/ws", Cwd: "/ws"},
		map[string][]int{"/ws/a.go": {10, 20}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s, f
}

func TestStartHandshakeOrderAndBreakpoints(t *testing.T) {
	s, f := start(t)
	got := f.Seen()
	want := []string{"initialize", "launch", "setBreakpoints", "configurationDone"}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("request order = %v, want prefix %v", got, want)
		}
	}
	lines, launch := f.Breakpoints("/ws/a.go"), f.Launch()
	if len(lines) != 2 || lines[0] != 10 || lines[1] != 20 {
		t.Fatalf("breakpoints = %v", lines)
	}
	if launch["mode"] != "debug" || launch["program"] != "/ws" || launch["cwd"] != "/ws" {
		t.Fatalf("launch args = %v", launch)
	}
	if !s.State().Started {
		t.Fatal("session should be started")
	}
}

func TestStopCapturesFramesLocalsOutput(t *testing.T) {
	s, _ := start(t)
	st := waitState(t, s, func(st State) bool { return st.Stopped }, "breakpoint stop")
	if st.Reason != "breakpoint" || st.ThreadID != 1 {
		t.Fatalf("stop = %+v", st)
	}
	if len(st.Frames) != 2 || st.Frames[0].Name != "main.run" || st.Frames[0].Line != 10 || st.Frames[0].Path != "/ws/a.go" {
		t.Fatalf("frames = %+v", st.Frames)
	}
	if len(st.Locals) != 1 || st.Locals[0] != (Variable{Name: "x", Value: "42", Type: "int"}) {
		t.Fatalf("locals = %+v", st.Locals)
	}
	if len(st.Output) != 2 || st.Output[0] != "hello" || st.Output[1] != "world" {
		t.Fatalf("output = %q", st.Output)
	}
}

func TestStepAndEvaluate(t *testing.T) {
	s, f := start(t)
	waitState(t, s, func(st State) bool { return st.Stopped }, "first stop")
	if err := s.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, s, func(st State) bool { return st.Stopped && st.Reason == "step" }, "step stop")
	if st.Frames[0].Line != 10 {
		t.Fatalf("frames after step = %+v", st.Frames)
	}
	out, err := s.Evaluate(context.Background(), "x*2")
	if err != nil || out != "84" {
		t.Fatalf("evaluate = %q err=%v", out, err)
	}
	for _, step := range []func(context.Context) error{s.StepIn, s.StepOut} {
		if err := step(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitState(t, s, func(st State) bool { return st.Stopped }, "stop")
	}
	_ = f
}

func TestContinueToExit(t *testing.T) {
	s, _ := start(t)
	waitState(t, s, func(st State) bool { return st.Stopped }, "first stop")
	if err := s.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, s, func(st State) bool { return st.Exited }, "exit")
	if st.ExitCode != 3 || st.Stopped {
		t.Fatalf("exit state = %+v", st)
	}
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		// Done closes only when the connection ends; the fake keeps it open.
	}
}

func TestSteppingWhileRunningRefuses(t *testing.T) {
	s, _ := start(t)
	waitState(t, s, func(st State) bool { return st.Stopped }, "first stop")
	_ = s.Continue(context.Background())
	waitState(t, s, func(st State) bool { return st.Exited }, "exit")
	if err := s.Next(context.Background()); err == nil {
		t.Fatal("stepping a finished program must refuse")
	}
	if _, err := s.Evaluate(context.Background(), "x"); err == nil {
		t.Fatal("evaluate while not stopped must refuse")
	}
}

func TestSetBreakpointsReportsUnverified(t *testing.T) {
	s, _ := start(t)
	bps, err := s.SetBreakpoints(context.Background(), "/ws/a.go", []int{12, 999})
	if err != nil || len(bps) != 2 {
		t.Fatalf("bps = %+v err=%v", bps, err)
	}
	if !bps[0].Verified || bps[1].Verified || bps[1].Message != "no code here" {
		t.Fatalf("verification = %+v", bps)
	}
}

func TestStartFailureIsNamed(t *testing.T) {
	c, f := newFake(t)
	f.Fail("configurationDone", "could not launch process: exec: not found")
	_, err := Start(context.Background(), c, LaunchConfig{Mode: "debug", Program: "/ws"}, nil)
	if err == nil || !strings.Contains(err.Error(), "could not launch process") {
		t.Fatalf("err = %v", err)
	}
}

func TestStopDisconnects(t *testing.T) {
	s, f := start(t)
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	seen := f.Seen()
	if seen[len(seen)-1] != "disconnect" {
		t.Fatalf("requests = %v", seen)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done should close after Stop")
	}
}

func TestConnectionLossMarksSessionEnded(t *testing.T) {
	c, f := newFake(t)
	s, err := Start(context.Background(), c, LaunchConfig{Mode: "debug", Program: "/ws"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, s, func(st State) bool { return st.Stopped }, "stop")
	f.Drop()
	st := waitState(t, s, func(st State) bool { return st.Exited }, "loss")
	if st.Err == "" {
		t.Fatalf("lost connection must say so, got %+v", st)
	}
	if _, err := c.Send("next", nil); err == nil {
		t.Fatal("calls on a dead connection must fail")
	}
}

func TestRefusedLaunchFailsFastWithTheAdaptersReason(t *testing.T) {
	c, f := newFake(t)
	f.Fail("launch", "could not launch process: native backend disabled during compilation")
	old := launchTimeout
	launchTimeout = 30 * time.Second // a refusal must NOT need this long
	defer func() { launchTimeout = old }()

	start := time.Now()
	_, err := Start(context.Background(), c, LaunchConfig{Mode: "debug", Program: "/ws"}, nil)
	if err == nil || !strings.Contains(err.Error(), "native backend disabled") {
		t.Fatalf("err = %v, want the adapter's own reason", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("refusal took %s; it must surface immediately, not after the launch timeout", took)
	}
}

// The debugger client and the toolchain must agree on the delve release.
func TestVerifiedDelveMatchesTheToolchainPin(t *testing.T) {
	if VerifiedDelve != toolchain.DelveVersion {
		t.Fatalf("dap.VerifiedDelve = %s, toolchain.DelveVersion = %s", VerifiedDelve, toolchain.DelveVersion)
	}
}
