package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/dap"
	"github.com/drjzlyan/dhi/internal/testutil/dapfake"
	"github.com/drjzlyan/dhi/internal/trust"
)

const pyTestSrc = "def test_a():\n    x = 1\n    assert x == 2\n"

// pyEditor opens alpha/test_calc.py with env as the tool PATH.
func pyEditor(t *testing.T, env []string, tools HostTools) (*Model, string) {
	t.Helper()
	ws, root := setupWorkspace(t)
	alpha := filepath.Join(root, "repos", "alpha")
	file := filepath.Join(alpha, "test_calc.py")
	if err := os.WriteFile(file, []byte(pyTestSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := []Option{WithTermEnv(env)}
	if tools != nil {
		opts = append(opts, WithHostTools(tools))
	}
	m := New("test", ws, opts...)
	m.Resize(120, 30)
	if m.OpenPaths(file) == 0 || m.active() == nil || m.active().Path() != file {
		t.Fatalf("could not open %s", file)
	}
	return m, file
}

// stubPytest writes a pytest that records its argv and writes a one-failure
// JUnit report to the --junitxml path.
func stubPytest(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	script := `for a in "$@"; do case "$a" in --junitxml=*) out="${a#--junitxml=}";; esac; done
cat > "$out" <<'XML'
<testsuites><testsuite time="0.1">
<testcase classname="test_calc" name="test_ok" file="test_calc.py" line="0"/>
<testcase classname="test_calc" name="test_a" file="test_calc.py" line="0"><failure message="assert 1 == 2">test_calc.py:3: in test_a
E   assert 1 == 2</failure></testcase>
</testsuite></testsuites>
XML
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "pytest"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir + ":/usr/bin:/bin"}
}

func TestTestCommandRunsPytestAndJumps(t *testing.T) {
	m, file := pyEditor(t, stubPytest(t), nil)
	runEx(m, "test")
	waitFor(t, m, func() bool { return m.mode == modeTests }, "failures overlay")
	out := plainView(m)
	for _, want := range []string{"test failures", "1 passed", "1 failed", "test_a", "test_calc.py:3", "assert 1 == 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("overlay missing %q:\n%s", want, out)
		}
	}
	feed(m, "enter")
	if m.mode != modeNav || m.active().Path() != file || m.active().Buffer().Cursor().Line != 2 {
		t.Fatalf("jump: mode=%v path=%s cursor=%+v", m.mode, m.active().Path(), m.active().Buffer().Cursor())
	}
}

func TestTestCommandPytestMissingRaisesInstallCard(t *testing.T) {
	m, _ := pyEditor(t, []string{"PATH=" + t.TempDir()}, echoTools{})
	runEx(m, "test")
	waitFor(t, m, func() bool { return m.toolCard != nil }, "install card")
	if m.toolCard.Tool != "pytest" || !strings.Contains(m.active().Message(), "pytest was not found in .venv/bin") {
		t.Fatalf("card=%+v msg=%q", m.toolCard, m.active().Message())
	}
}

func TestTestCommandUnsupportedFileIsNamed(t *testing.T) {
	m := testEditor(t, nil)
	if msg, _ := m.testCommand("test"); msg == "" { // app.go is fine; sanity
		t.Fatal("expected a message")
	}
	other := filepath.Join(filepath.Dir(m.active().Path()), "notes.rs")
	if err := os.WriteFile(other, []byte("fn main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.testing = nil
	m.OpenPaths(other)
	if msg, _ := m.testCommand("test"); !strings.Contains(msg, "no test runner for .rs") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestRestrictedBlocksPythonTestsAndDebug(t *testing.T) {
	ws, root := setupWorkspace(t)
	alpha := filepath.Join(root, "repos", "alpha")
	file := filepath.Join(alpha, "test_calc.py")
	_ = os.WriteFile(file, []byte(pyTestSrc), 0o644)
	st := trust.New(filepath.Join(t.TempDir(), "trust.json"))
	m := New("test", ws, WithTrust(st), WithTermEnv(stubPytest(t)))
	m.Resize(100, 30)
	m.OpenPaths(file)
	if msg, _ := m.testCommand("test"); !strings.Contains(msg, "restricted: trust this folder to run tests") || m.testing != nil {
		t.Errorf("test: %q", msg)
	}
	started := false
	m.dapStart = func(context.Context, string, []string) (*dap.Client, error) {
		started = true
		return nil, os.ErrInvalid
	}
	if msg := m.startDebug(""); !strings.Contains(msg, "restricted: trust this folder to debug") || started {
		t.Errorf("debug: %q started=%v", msg, started)
	}
}

func TestDebugPythonFileLaunchesDebugpyBody(t *testing.T) {
	m, file := pyEditor(t, nil, nil)
	var fake *dapfake.Adapter
	m.dapStart = func(context.Context, string, []string) (*dap.Client, error) {
		conn, a := dapfake.New(t)
		a.StopAt(file, 3)
		fake = a
		return dap.New(conn), nil
	}
	feed(m, "down", "down") // line 3
	runEx(m, "break")
	runEx(m, "debug adds")
	waitFor(t, m, func() bool { return m.mode == modeDebug }, "stop at breakpoint")
	l := fake.Launch()
	args, _ := l["args"].([]any)
	if l["module"] != "pytest" || l["type"] != "python" || len(args) != 3 || args[0] != file || args[2] != "adds" {
		t.Fatalf("launch = %v", l)
	}
	if bl := fake.Breakpoints(file); len(bl) != 1 || bl[0] != 3 {
		t.Fatalf("breakpoints = %v", bl)
	}
}

func TestDebugMissingAdapterToolRaisesInstallCard(t *testing.T) {
	m, _ := pyEditor(t, nil, echoTools{})
	m.dapStart = func(context.Context, string, []string) (*dap.Client, error) { return nil, dap.ErrNoDebugpy }
	runEx(m, "debug")
	waitFor(t, m, func() bool { return m.toolCard != nil }, "install card")
	if m.toolCard.Tool != "debugpy" {
		t.Fatalf("card = %+v", m.toolCard)
	}
}

func TestDebugUnsupportedLanguageIsNamed(t *testing.T) {
	m, _ := pyEditor(t, nil, nil)
	other := filepath.Join(filepath.Dir(m.active().Path()), "a.ts")
	_ = os.WriteFile(other, []byte("let x = 1\n"), 0o644)
	m.OpenPaths(other)
	if msg := m.startDebug(""); !strings.Contains(msg, "no debug adapter for .ts") {
		t.Fatalf("msg = %q", msg)
	}
}
