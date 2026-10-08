package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGo writes an executable `go` that prints lines of go-test JSON.
func stubGo(t *testing.T, lines ...string) []string {
	t.Helper()
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	for _, l := range lines {
		sb.WriteString("printf '%s\\n' '" + l + "'\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(sb.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir}
}

func testEditor(t *testing.T, env []string) *Model {
	t.Helper()
	ws, _ := setupWorkspace(t)
	m := New("test", ws, WithTermEnv(env))
	m.Resize(120, 30)
	feed(m, "enter", "down", "down", "enter") // open alpha/app.go
	if m.active() == nil || !strings.HasSuffix(m.active().Path(), "app.go") {
		t.Fatalf("expected app.go open, got %v", m.active())
	}
	return m
}

func runEx(m *Model, cmd string) {
	feed(m, ":")
	typeKeys(m, cmd)
	feed(m, "enter")
}

func TestTestCommandRedRunOpensFailuresAndJumps(t *testing.T) {
	env := stubGo(t,
		`{"Action":"output","Package":"ex/alpha","Test":"TestBad","Output":"    app.go:1: boom\n"}`,
		`{"Action":"fail","Package":"ex/alpha","Test":"TestBad","Elapsed":0}`,
		`{"Action":"pass","Package":"ex/alpha","Test":"TestFine","Elapsed":0}`,
	)
	m := testEditor(t, env)
	runEx(m, "test")
	if got := m.active().Message(); !strings.Contains(got, "running tests") {
		t.Fatalf("message = %q", got)
	}
	waitFor(t, m, func() bool { return m.mode == modeTests }, "failures overlay")
	out := plainView(m)
	for _, want := range []string{"test failures", "1 passed", "1 failed", "TestBad", "app.go:1", "boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("overlay missing %q:\n%s", want, out)
		}
	}
	if ctx, label := m.StatusContext(); ctx != "tests" || label != "TESTS" {
		t.Fatalf("status = %q/%q", ctx, label)
	}
	m.active().Buffer().SetCursor(m.active().Buffer().Cursor()) // no-op; cursor starts at 0
	feed(m, "enter")
	if m.mode != modeNav || m.active().Buffer().Cursor().Line != 0 {
		t.Fatalf("jump failed: mode=%v cursor=%+v", m.mode, m.active().Buffer().Cursor())
	}
}

func TestTestCommandGreenRunIsOneLine(t *testing.T) {
	env := stubGo(t, `{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0}`)
	m := testEditor(t, env)
	runEx(m, "test all")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "tests passed") }, "green message")
	if m.mode == modeTests {
		t.Fatal("a green run must not open the overlay")
	}
}

func TestTestCommandRefusals(t *testing.T) {
	m := testEditor(t, nil) // no toolchain PATH at all
	runEx(m, "test")
	waitFor(t, m, func() bool { return strings.Contains(m.active().Message(), "not found on the DHI toolchain PATH") }, "named missing-go refusal")

	n := newEditor(t) // nothing open
	runEx(n, "test")
	if got := n.active(); got != nil {
		t.Fatalf("no buffer expected, got %v", got)
	}
}

func TestTestRerunAndEsc(t *testing.T) {
	env := stubGo(t,
		`{"Action":"output","Package":"p","Test":"TestBad","Output":"    app.go:1: boom\n"}`,
		`{"Action":"fail","Package":"p","Test":"TestBad","Elapsed":0}`)
	m := testEditor(t, env)
	runEx(m, "test")
	waitFor(t, m, func() bool { return m.mode == modeTests }, "overlay")
	feed(m, "esc")
	if m.mode != modeNav {
		t.Fatal("esc closes the overlay")
	}
	runEx(m, "test")
	waitFor(t, m, func() bool { return m.mode == modeTests }, "overlay again")
	feed(m, "r")
	if m.mode != modeNav || m.testing == nil || !m.testing.running {
		t.Fatalf("r should rerun: mode=%v testing=%+v", m.mode, m.testing)
	}
	waitFor(t, m, func() bool { return m.mode == modeTests }, "overlay after rerun")
}
