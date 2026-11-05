package dap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drjzlyan/dhi/internal/testutil/dapfake"
)

func TestAdapterTable(t *testing.T) {
	for path, want := range map[string]string{"/w/a.go": "Go", "/w/a.py": "Python", "/w/a.ts": ""} {
		a, ok := AdapterFor(path)
		if (want == "") == ok || a.Lang != want {
			t.Errorf("AdapterFor(%s) = %q %v", path, a.Lang, ok)
		}
	}
}

func TestPythonLaunchConfigs(t *testing.T) {
	script := pythonLaunch("/w/app/main.py", "/w", "").Raw
	if script["program"] != "/w/app/main.py" || script["module"] != nil || script["cwd"] != "/w" || script["console"] != "internalConsole" {
		t.Fatalf("script launch = %v", script)
	}
	test := pythonLaunch("/w/tests/test_a.py", "/w", "adds").Raw
	args, _ := test["args"].([]string)
	if test["module"] != "pytest" || test["program"] != nil || strings.Join(args, " ") != "/w/tests/test_a.py -k adds" {
		t.Fatalf("test launch = %v", test)
	}
	if pythonLaunch("/w/a_test.py", "/w", "").Raw["module"] != "pytest" {
		t.Fatal("*_test.py is a pytest file")
	}
}

// The session sends a table row's Raw body verbatim (no delve fields).
func TestStartSendsRawLaunchBody(t *testing.T) {
	conn, fake := dapfake.New(t)
	c := New(conn)
	cfg := pythonLaunch("/w/main.py", "/w", "")
	s, err := Start(context.Background(), c, cfg, map[string][]int{"/w/main.py": {3}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	got := fake.Launch()
	if got["request"] != "launch" || got["program"] != "/w/main.py" || got["type"] != "python" {
		t.Fatalf("launch = %v", got)
	}
	if got["mode"] != nil || got["outputMode"] != nil {
		t.Fatalf("delve-only fields leaked into the python launch: %v", got)
	}
	if bl := fake.Breakpoints("/w/main.py"); len(bl) != 1 || bl[0] != 3 {
		t.Fatalf("breakpoints = %v", bl)
	}
}

func stubPython(t *testing.T, script string) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "python3"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir + ":/usr/bin:/bin"}
}

func TestStartDebugpyNamesWhatIsMissing(t *testing.T) {
	_, err := StartDebugpy(context.Background(), t.TempDir(), []string{"PATH=" + t.TempDir()})
	var mt *MissingToolError
	if !errors.As(err, &mt) || mt.Tool != "python" {
		t.Fatalf("no python: %v", err)
	}
	env := stubPython(t, "exit 1\n") // `import debugpy.adapter` fails
	_, err = StartDebugpy(context.Background(), t.TempDir(), env)
	if !errors.As(err, &mt) || mt.Tool != "debugpy" {
		t.Fatalf("no debugpy: %v", err)
	}
}

// A stub python3 whose adapter answers `initialize` with a canned frame
// proves the stdio wiring and process ownership.
func TestStartDebugpyTalksOverStdio(t *testing.T) {
	body := `{"seq":1,"type":"response","request_seq":1,"success":true,"command":"initialize"}`
	env := stubPython(t, `case "$1" in
-c) exit 0;;
-m) test "$2" = debugpy.adapter || exit 3
    printf 'Content-Length: `+strconv.Itoa(len(body))+`\r\n\r\n%s' '`+body+`'
    exec sleep 30;;
esac
`)
	c, err := StartDebugpy(context.Background(), t.TempDir(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Call(ctx, "initialize", map[string]any{}, nil); err != nil {
		t.Fatalf("initialize over stdio: %v", err)
	}
}
