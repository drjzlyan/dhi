package testrun

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Target says what one `:test` run covers. Dir is the workspace member
// root; File is the open file (its package for Go, itself for the others).
type Target struct {
	Dir     string
	File    string
	Pattern string // restrict to tests matching this name
	All     bool   // the whole member
}

// Language is one row of the per-language runner table (F-177). Each Run
// resolves its tool from the user's PATH (env) and normalises the result
// into Report/Failure, so the failures overlay, jump, "fix this test" and
// the restricted-mode gate stay language-blind. A missing tool is named in
// Report.MissingTool for the install card, never replaced by a guess.
type Language struct {
	Name string
	Exts []string
	Run  func(ctx context.Context, env []string, t Target) Report
}

// Languages is the table, in lookup order.
var Languages = []Language{
	{Name: "Go", Exts: []string{".go"}, Run: runGo},
	{Name: "Python", Exts: []string{".py"}, Run: runPytest},
	{Name: "JavaScript", Exts: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"}, Run: runJS},
}

// ForPath finds the language whose runner handles path.
func ForPath(path string) (Language, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, l := range Languages {
		for _, e := range l.Exts {
			if e == ext {
				return l, true
			}
		}
	}
	return Language{}, false
}

// runGo maps a Target to `go test -json` arguments: the file's package, a
// -run filter inside it, or ./... over the member.
func runGo(ctx context.Context, env []string, t Target) Report {
	dir, args := filepath.Dir(t.File), []string{"."}
	switch {
	case t.All:
		dir, args = t.Dir, []string{"./..."}
	case t.Pattern != "":
		args = []string{"-run", t.Pattern, "."}
	}
	return Run(ctx, dir, env, args...)
}

// execOut runs bin with args in dir and returns stdout and stderr. A
// non-zero exit is the caller's to interpret: red runs exit non-zero.
func execOut(ctx context.Context, dir string, env []string, bin string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = dir, env
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	return so.Bytes(), se.Bytes(), err
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string { return ansiSeq.ReplaceAllString(s, "") }

// missing is the report for an absent tool: Err names it, MissingTool lets
// the editor raise the install card.
func missing(tool string, err error) Report {
	return Report{Err: err.Error(), MissingTool: tool}
}

// venvDirs are the project virtualenv locations looked at, in order,
// before the PATH (F-177). Windows layouts (Scripts\) are not supported.
var venvDirs = []string{".venv", "venv"}

// LookPython finds a Python tool in the member's virtualenv (.venv/bin,
// then venv/bin), then on the user's PATH under pathName. The error names
// that order so the missing-tool message is exact.
func LookPython(root, venvName, pathName string, env []string) (string, error) {
	order := []string{}
	if runtime.GOOS != "windows" {
		for _, v := range venvDirs {
			p := filepath.Join(root, v, "bin", venvName)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p, nil
			}
			order = append(order, filepath.Join(v, "bin"))
		}
	}
	if p, err := LookPath(pathName, env); err == nil {
		return p, nil
	}
	order = append(order, "your PATH")
	msg := pathName + " was not found in " + strings.Join(order, ", then ")
	if runtime.GOOS == "windows" {
		msg += " (project virtualenvs are not looked up on Windows)"
	}
	return "", errors.New(msg)
}
