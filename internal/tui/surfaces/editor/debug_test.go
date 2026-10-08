package editor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/dap"
	"github.com/drjzlyan/dhi/internal/testutil/dapfake"
)

func TestBreakpointToggleAndGutter(t *testing.T) {
	m := testEditor(t, nil)
	path := m.active().Path()
	runEx(m, "break")
	if got := m.active().Message(); !strings.Contains(got, "breakpoint set at app.go:1") {
		t.Fatalf("message = %q", got)
	}
	if bl := m.breakpoints[path]; len(bl) != 1 || bl[0] != 1 {
		t.Fatalf("breakpoints = %v", m.breakpoints)
	}
	if !strings.Contains(plainView(m), "1●") {
		t.Fatalf("gutter marker missing:\n%s", plainView(m))
	}
	runEx(m, "break")
	if got := m.active().Message(); !strings.Contains(got, "removed") || len(m.breakpoints) != 0 {
		t.Fatalf("toggle off: %q %v", got, m.breakpoints)
	}
}

func TestDebugSessionEndToEnd(t *testing.T) {
	m := testEditor(t, nil)
	path := m.active().Path()
	var fake *dapfake.Adapter
	m.dapStart = func(context.Context, string, []string) (*dap.Client, error) {
		conn, a := dapfake.New(t)
		a.StopAt(path, 1)
		fake = a
		return dap.New(conn), nil
	}
	runEx(m, "break")
	runEx(m, "debug")
	waitFor(t, m, func() bool { return m.mode == modeDebug }, "stop at breakpoint")

	out := plainView(m)
	for _, want := range []string{"debugger — breakpoint", "main.run", "app.go:1", "x int = 42", "hello", "c continue"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q:\n%s", want, out)
		}
	}
	if ctx, label := m.StatusContext(); ctx != "debugger" || label != "DEBUG" {
		t.Fatalf("status = %q/%q", ctx, label)
	}
	launch := fake.Launch()
	if launch["mode"] != "debug" || launch["program"] != filepath.Dir(path) {
		t.Fatalf("launch = %v", launch)
	}
	if bl := fake.Breakpoints(path); len(bl) != 1 || bl[0] != 1 {
		t.Fatalf("adapter breakpoints = %v", bl)
	}

	// Step: a second stop re-opens the panel even without `continued`.
	feed(m, "esc")
	if m.mode != modeNav {
		t.Fatal("esc hides the panel")
	}
	if !strings.Contains(plainView(m), "1▶") {
		t.Fatalf("stop marker missing:\n%s", plainView(m))
	}
	feed(m, "ctrl+d")
	if m.mode != modeDebug {
		t.Fatal("ctrl+d reopens the panel")
	}
	feed(m, "n")
	waitFor(t, m, func() bool { return m.dbg != nil && m.dbg.st.StopSeq == 2 && m.mode == modeDebug }, "step stop")
	if !strings.Contains(plainView(m), "debugger — step") {
		t.Fatalf("step panel:\n%s", plainView(m))
	}

	// State for agents, then evaluate, then run to exit.
	state, err := m.DebugState()
	if err != nil || !strings.Contains(state, "stopped: step") || !strings.Contains(state, "x int = 42") || !strings.Contains(state, "#0 main.run") {
		t.Fatalf("DebugState = %q err=%v", state, err)
	}
	feed(m, "esc")
	runEx(m, "eval x*2")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "x*2 = 84") }, "eval result")
	runEx(m, "cont")
	waitFor(t, m, func() bool { return m.dbg == nil }, "session end")
	if got := m.active().Message(); !strings.Contains(got, "program exited (code 3)") {
		t.Fatalf("exit message = %q", got)
	}
	if s, _ := m.DebugState(); s != "no debug session" {
		t.Fatalf("DebugState after exit = %q", s)
	}
}

func TestDebugStopCommandDisconnects(t *testing.T) {
	m := testEditor(t, nil)
	path := m.active().Path()
	var fake *dapfake.Adapter
	m.dapStart = func(context.Context, string, []string) (*dap.Client, error) {
		conn, a := dapfake.New(t)
		a.StopAt(path, 1)
		fake = a
		return dap.New(conn), nil
	}
	runEx(m, "debug")
	waitFor(t, m, func() bool { return m.mode == modeDebug }, "stop")
	feed(m, "x")
	if m.dbg != nil || m.mode != modeNav {
		t.Fatal("x stops the session and closes the panel")
	}
	waitFor(t, m, func() bool {
		seen := fake.Seen()
		return len(seen) > 0 && seen[len(seen)-1] == "disconnect"
	}, "disconnect request")
}

func TestDebugRefusals(t *testing.T) {
	m := testEditor(t, nil) // no dlv on the (empty) toolchain PATH
	for _, c := range []struct{ cmd, want string }{
		{"next", "no session"},
		{"stop", "no session"},
		{"eval x", "needs a stopped debug session"},
	} {
		runEx(m, c.cmd)
		if got := m.active().Message(); !strings.Contains(got, c.want) {
			t.Errorf(":%s → %q, want %q", c.cmd, got, c.want)
		}
	}
	runEx(m, "debug")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "delve adapter") }, "named dlv refusal")
	if m.dbg != nil {
		t.Fatal("a failed start must clear the session")
	}

	// A launch failure from the adapter is shown, not swallowed.
	path := m.active().Path()
	m.dapStart = func(context.Context, string, []string) (*dap.Client, error) {
		conn, a := dapfake.New(t)
		a.StopAt(path, 1)
		a.Fail("configurationDone", "could not launch process")
		return dap.New(conn), nil
	}
	runEx(m, "debug")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "could not launch process") }, "launch failure")
}

func TestBreakpointRefusals(t *testing.T) {
	n := newEditor(t)
	runEx(n, "break")
	if n.active() != nil {
		t.Fatal("no buffer expected")
	}
}
