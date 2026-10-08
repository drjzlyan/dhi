// Package testrun runs `go test -json` and turns the event stream into a
// compact report the editor can show and jump through (F-040). The go
// binary is resolved from the supplied environment's PATH — the hermetic
// toolchain shims come first — never from the host PATH of this process.
package testrun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Failure is one failed test (or a package that failed to build).
type Failure struct {
	Package string
	Test    string // "" for a build failure
	File    string // absolute when resolvable, else as printed
	Line    int
	Message string
	Output  []string
}

// Report is the outcome of one run.
type Report struct {
	Passed, Failed, Skipped int
	Failures                []Failure
	Elapsed                 time.Duration
	// Err is a run-level problem (go missing, no output); a normal red
	// test run has an empty Err and Failures instead.
	Err string
}

// event is one `go test -json` record.
type event struct {
	Action  string
	Package string
	Test    string
	Output  string
	Elapsed float64
}

var (
	// testLine matches `    foo_test.go:12: message` log lines.
	testLine = regexp.MustCompile(`^\s*([\w./\\-]+\.go):(\d+):(?:\d+:)?\s*(.*)$`)
	// buildLine matches compiler output `./foo.go:3:2: undefined: x`.
	buildLine = regexp.MustCompile(`^([\w./\\-]+\.go):(\d+):\d+:\s*(.*)$`)
)

// Parse reads a `go test -json` stream. root is the directory the run
// started in, used to resolve file names to absolute paths.
func Parse(r io.Reader, root string) Report {
	rep := Report{}
	outputs := map[string][]string{} // key: pkg + "\x00" + test
	failed := map[string]bool{}
	var order []string
	key := func(pkg, test string) string { return pkg + "\x00" + test }

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var elapsed float64
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // non-JSON noise (e.g. toolchain download lines)
		}
		k := key(ev.Package, ev.Test)
		switch ev.Action {
		case "output":
			outputs[k] = append(outputs[k], strings.TrimRight(ev.Output, "\n"))
		case "pass":
			if ev.Test != "" {
				rep.Passed++
			} else {
				elapsed += ev.Elapsed
			}
		case "skip":
			if ev.Test != "" {
				rep.Skipped++
			}
		case "fail":
			if ev.Test == "" {
				// Package-level fail: only interesting when no test in
				// it failed (a build error or a panic outside a test).
				if !failedInPkg(failed, ev.Package) {
					failed[k] = true
					order = append(order, k)
				}
				elapsed += ev.Elapsed
				continue
			}
			rep.Failed++
			failed[k] = true
			order = append(order, k)
		}
	}
	rep.Elapsed = time.Duration(elapsed * float64(time.Second))
	seen := map[string]bool{}
	for _, k := range order {
		if seen[k] {
			continue
		}
		seen[k] = true
		parts := strings.SplitN(k, "\x00", 2)
		pkg, test := parts[0], parts[1]
		f := Failure{Package: pkg, Test: test, Output: outputs[k]}
		f.File, f.Line, f.Message = locate(outputs[k], test == "", root, pkg)
		if f.Message == "" {
			f.Message = firstUseful(outputs[k])
		}
		rep.Failures = append(rep.Failures, f)
		if test == "" {
			rep.Failed++ // a build failure counts as one failure
		}
	}
	sort.SliceStable(rep.Failures, func(i, j int) bool { return rep.Failures[i].Package < rep.Failures[j].Package })
	return rep
}

func failedInPkg(failed map[string]bool, pkg string) bool {
	for k := range failed {
		if strings.HasPrefix(k, pkg+"\x00") {
			return true
		}
	}
	return false
}

// locate finds the first file:line in a failure's output.
func locate(lines []string, build bool, root, pkg string) (file string, line int, msg string) {
	re := testLine
	if build {
		re = buildLine
	}
	for _, l := range lines {
		m := re.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n := 0
		_, _ = fmt.Sscanf(m[2], "%d", &n)
		return resolveFile(root, pkg, m[1]), n, strings.TrimSpace(m[3])
	}
	return "", 0, ""
}

func firstUseful(lines []string) string {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "=== ") || strings.HasPrefix(t, "--- ") ||
			t == "FAIL" || strings.HasPrefix(t, "FAIL\t") || strings.HasPrefix(t, "exit status") {
			continue
		}
		return t
	}
	return ""
}

// resolveFile maps a printed file name to an absolute path. Test logs
// print only the base name relative to the package directory, so the
// package's import path suffix is tried under root, longest first.
func resolveFile(root, pkg, name string) string {
	name = filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(name), "./"))
	if filepath.IsAbs(name) {
		return name
	}
	if p := filepath.Join(root, name); exists(p) {
		return p
	}
	parts := strings.Split(pkg, "/")
	for i := 0; i < len(parts); i++ {
		if p := filepath.Join(append([]string{root}, append(parts[i:], name)...)...); exists(p) {
			return p
		}
	}
	return name
}

func exists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// LookPath finds name on the PATH carried in env (KEY=VALUE pairs).
func LookPath(name string, env []string) (string, error) {
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			continue
		}
		for _, dir := range filepath.SplitList(strings.TrimPrefix(kv, "PATH=")) {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s not found on the DHI toolchain PATH", name)
}

// Run executes `go test -json <args...>` in dir and reports. A non-zero
// exit with a parseable stream is a normal red run, not an error.
func Run(ctx context.Context, dir string, env []string, args ...string) Report {
	goBin, err := LookPath("go", env)
	if err != nil {
		return Report{Err: err.Error()}
	}
	cmd := exec.CommandContext(ctx, goBin, append([]string{"test", "-json"}, args...)...)
	cmd.Dir = dir
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Report{Err: err.Error()}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Report{Err: "go test: " + err.Error()}
	}
	rep := Parse(stdout, dir)
	werr := cmd.Wait()
	if rep.Passed+rep.Failed+rep.Skipped == 0 && len(rep.Failures) == 0 {
		switch {
		case ctx.Err() != nil:
			rep.Err = "cancelled"
		case werr != nil:
			rep.Err = "go test: " + firstLine(stderr.String(), werr.Error())
		default:
			rep.Err = "no tests ran"
		}
	}
	return rep
}

func firstLine(s, fallback string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return fallback
}
