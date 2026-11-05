package dap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/drjzlyan/dhi/internal/testrun"
)

// MissingToolError says a debug adapter's tool is absent. Tool is the
// hosttools name the editor raises the install card for (F-177); a missing
// adapter is never replaced by a guess (ADR-0011).
type MissingToolError struct {
	Tool string
	Msg  string
}

func (e *MissingToolError) Error() string { return e.Msg }

// Adapter is one row of the per-language debug table (F-177). The wire
// protocol is shared; a row only says how to start the adapter and what
// the launch request carries for a file.
type Adapter struct {
	Lang string
	Exts []string
	// Start launches the adapter (tools resolved from env's PATH, the
	// user's own) in dir. Closing the Client stops it.
	Start func(ctx context.Context, dir string, env []string) (*Client, error)
	// Launch is the LaunchConfig for debugging file; pattern narrows a
	// test file to matching tests. root is the workspace member.
	Launch func(file, root, pattern string) LaunchConfig
}

// Adapters is the table, in lookup order. JavaScript/TypeScript has no row:
// its debug adapter (vscode-js-debug) is not a PATH tool, and DHI does not
// download one (ADR-0044).
var Adapters = []Adapter{
	{Lang: "Go", Exts: []string{".go"}, Start: StartDelve, Launch: goLaunch},
	{Lang: "Python", Exts: []string{".py"}, Start: StartDebugpy, Launch: pythonLaunch},
}

// AdapterFor finds the row that debugs path.
func AdapterFor(path string) (Adapter, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, a := range Adapters {
		for _, e := range a.Exts {
			if e == ext {
				return a, true
			}
		}
	}
	return Adapter{}, false
}

func goLaunch(file, root, _ string) LaunchConfig {
	return LaunchConfig{Mode: "debug", Program: filepath.Dir(file), Cwd: root}
}

// pythonLaunch debugs a test file through pytest (optionally one test via
// -k) and any other .py file as a script. debugpy's internal console keeps
// the debuggee's output in DAP `output` events; justMyCode is off so a
// stop inside a library is reachable by stepping.
func pythonLaunch(file, root, pattern string) LaunchConfig {
	raw := map[string]any{
		"type": "python", "name": "dhi", "cwd": root,
		"console": "internalConsole", "justMyCode": false, "subProcess": false,
	}
	if isPyTestFile(file) {
		args := []string{file}
		if pattern != "" {
			args = append(args, "-k", pattern)
		}
		raw["module"], raw["args"] = "pytest", args
	} else {
		raw["program"] = file
	}
	return LaunchConfig{Raw: raw, Program: file, Cwd: root}
}

func isPyTestFile(file string) bool {
	b := filepath.Base(file)
	return strings.HasPrefix(b, "test_") && strings.HasSuffix(b, ".py") || strings.HasSuffix(b, "_test.py")
}

// ErrNoDebugpy says python is there but debugpy is not importable.
var ErrNoDebugpy = &MissingToolError{Tool: "debugpy", Msg: "debugging Python needs debugpy, which python3 cannot import"}

// stdioConn is an adapter that speaks DAP on its stdin/stdout and owns
// the process.
type stdioConn struct {
	io.Reader
	io.WriteCloser
	cmd *exec.Cmd
}

func (s *stdioConn) Close() error {
	err := s.WriteCloser.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.cmd.Wait()
	return err
}

// StartDebugpy launches `python3 -m debugpy.adapter` (python3 from env's
// PATH) in dir and connects over its stdio.
func StartDebugpy(ctx context.Context, dir string, env []string) (*Client, error) {
	py, err := testrun.LookPython(dir, "python", "python3", env)
	if err != nil {
		return nil, &MissingToolError{Tool: "python", Msg: "debugging Python needs python: " + err.Error()}
	}
	chk, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	probe := exec.CommandContext(chk, py, "-c", "import debugpy.adapter")
	probe.Dir, probe.Env = dir, env
	if err := probe.Run(); err != nil {
		if errors.Is(chk.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("python3 did not answer within 10s while checking for debugpy")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrNoDebugpy
	}
	cmd := exec.Command(py, "-m", "debugpy.adapter")
	cmd.Dir, cmd.Env = dir, env
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("debugpy: %w", err)
	}
	return New(&stdioConn{Reader: out, WriteCloser: in, cmd: cmd}), nil
}
