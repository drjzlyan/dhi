package testrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// jsRunners are tried in order: the project's own node_modules/.bin (what
// `npm test` would run), then the user's PATH. vitest's JSON reporter
// emits the same shape as jest --json, so one parser serves both.
var jsRunners = []string{"vitest", "jest"}

// runJS runs vitest or jest with a JSON report on stdout.
func runJS(ctx context.Context, env []string, t Target) Report {
	var bin, name string
	for _, n := range jsRunners {
		if p := localBin(t.Dir, n); p != "" {
			bin, name = p, n
			break
		}
		if p, err := LookPath(n, env); err == nil {
			bin, name = p, n
			break
		}
	}
	if bin == "" {
		return missing("vitest", errors.New("neither vitest nor jest was found in the project or on your PATH"))
	}
	var args []string
	if name == "vitest" {
		args = []string{"run", "--reporter=json"}
	} else {
		args = []string{"--json", "--testLocationInResults"}
	}
	if t.Pattern != "" && !t.All {
		args = append(args, "-t", t.Pattern)
	}
	if !t.All && t.File != "" {
		args = append(args, t.File)
	}
	start := time.Now()
	stdout, stderr, runErr := execOut(ctx, t.Dir, env, bin, args...)
	rep, ok := ParseJS(stdout, t.Dir)
	if !ok {
		switch {
		case ctx.Err() != nil:
			return Report{Err: "cancelled"}
		case runErr != nil:
			return Report{Err: name + ": " + firstLine(stripANSI(string(stderr)+"\n"+string(stdout)), runErr.Error())}
		}
		return Report{Err: name + " wrote no JSON report"}
	}
	rep.Elapsed = time.Since(start)
	if rep.Passed+rep.Failed+rep.Skipped == 0 && len(rep.Failures) == 0 {
		rep.Err = "no tests ran"
	}
	return rep
}

// localBin finds node_modules/.bin/<name> in dir or a parent.
func localBin(dir, name string) string {
	for d := dir; d != "" && d != filepath.Dir(d); d = filepath.Dir(d) {
		p := filepath.Join(d, "node_modules", ".bin", name)
		if exists(p) {
			return p
		}
	}
	return ""
}

type jsReport struct {
	TestResults []struct {
		Name             string  `json:"name"`
		Status           string  `json:"status"`
		Message          string  `json:"message"`
		AssertionResults []jsAss `json:"assertionResults"`
	} `json:"testResults"`
}

type jsAss struct {
	FullName        string   `json:"fullName"`
	Title           string   `json:"title"`
	AncestorTitles  []string `json:"ancestorTitles"`
	Status          string   `json:"status"`
	FailureMessages []string `json:"failureMessages"`
	Location        *struct {
		Line int `json:"line"`
	} `json:"location"`
}

// jsStack finds `(/abs/file.ts:12:5)` or `/abs/file.ts:12:5` in a stack.
var jsStack = regexp.MustCompile(`(?:\(|\s|^)((?:[A-Za-z]:)?[^\s():]+\.[cm]?[jt]sx?):(\d+):\d+`)

// ParseJS reads a jest/vitest JSON report from r (tolerating text before
// the object, e.g. a banner). ok is false when no report object is found.
func ParseJS(raw []byte, root string) (Report, bool) {
	i := bytes.Index(raw, []byte("{"))
	for i >= 0 {
		var jr jsReport
		if err := json.NewDecoder(bytes.NewReader(raw[i:])).Decode(&jr); err == nil && jr.TestResults != nil {
			return jsToReport(jr, root), true
		}
		next := bytes.Index(raw[i+1:], []byte("\n{"))
		if next < 0 {
			break
		}
		i += next + 2
	}
	return Report{}, false
}

func jsToReport(jr jsReport, root string) Report {
	rep := Report{}
	for _, tr := range jr.TestResults {
		hadFail := false
		for _, a := range tr.AssertionResults {
			switch a.Status {
			case "passed":
				rep.Passed++
			case "failed":
				rep.Failed++
				hadFail = true
				rep.Failures = append(rep.Failures, jsFailure(tr.Name, a, root))
			default: // pending, skipped, todo, disabled
				rep.Skipped++
			}
		}
		if tr.Status == "failed" && !hadFail {
			// The suite itself failed (syntax error, import error): one
			// failure for the file, like a Go build failure.
			rep.Failed++
			f := Failure{Package: shortRel(root, tr.Name)}
			f.Output = splitClean(tr.Message)
			f.File, f.Line = jsWhere(tr.Name, f.Output, root)
			f.Message = firstUseful(f.Output)
			rep.Failures = append(rep.Failures, f)
		}
	}
	return rep
}

func jsFailure(file string, a jsAss, root string) Failure {
	name := a.FullName
	if name == "" {
		name = strings.Join(append(append([]string(nil), a.AncestorTitles...), a.Title), " ")
	}
	f := Failure{Package: shortRel(root, file), Test: name}
	for _, m := range a.FailureMessages {
		f.Output = append(f.Output, splitClean(m)...)
	}
	f.File, f.Line = jsWhere(file, f.Output, root)
	if a.Location != nil && a.Location.Line > 0 {
		f.File, f.Line = file, a.Location.Line
		if !filepath.IsAbs(f.File) {
			f.File = resolveFile(root, "", file)
		}
	}
	f.Message = firstUseful(f.Output)
	return f
}

// jsWhere locates the failure in its stack: the first frame in the test
// file, else the first frame outside node_modules.
func jsWhere(file string, lines []string, root string) (string, int) {
	var other string
	var otherLine int
	for _, l := range lines {
		m := jsStack.FindStringSubmatch(l)
		if m == nil || strings.Contains(m[1], "node_modules") {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		if sameFile(m[1], file) {
			return resolveFile(root, "", m[1]), n
		}
		if other == "" {
			other, otherLine = m[1], n
		}
	}
	if other != "" {
		return resolveFile(root, "", other), otherLine
	}
	if file != "" {
		return resolveFile(root, "", file), 0
	}
	return "", 0
}

func splitClean(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(stripANSI(s), "\n"), "\n") {
		out = append(out, strings.TrimRight(l, "\r"))
	}
	return out
}

func shortRel(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}
