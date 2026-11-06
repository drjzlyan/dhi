package testrun

import (
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// runPytest runs pytest from the user's PATH and reads its JUnit XML (a
// temp file DHI creates and removes) into a Report. xunit1 is requested
// because only that family carries the file/line attributes.
func runPytest(ctx context.Context, env []string, t Target) Report {
	bin, err := LookPython(t.Dir, "pytest", "pytest", env)
	if err != nil {
		return missing("pytest", err)
	}
	tmp, err := os.MkdirTemp("", "dhi-pytest-")
	if err != nil {
		return Report{Err: err.Error()}
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	xmlPath := filepath.Join(tmp, "junit.xml")
	args := []string{"--junitxml=" + xmlPath, "-o", "junit_family=xunit1", "--tb=short", "-q", "-p", "no:cacheprovider"}
	if t.Pattern != "" && !t.All {
		args = append(args, "-k", t.Pattern)
	}
	if !t.All && t.File != "" {
		args = append(args, t.File)
	}
	stdout, stderr, runErr := execOut(ctx, t.Dir, env, bin, args...)
	data, rerr := os.ReadFile(xmlPath)
	if rerr != nil {
		// No report: usage error, crash before collection, or cancelled.
		switch {
		case ctx.Err() != nil:
			return Report{Err: "cancelled"}
		case runErr != nil:
			return Report{Err: "pytest: " + firstLine(string(stderr)+"\n"+string(stdout), runErr.Error())}
		}
		return Report{Err: "pytest wrote no report"}
	}
	rep, perr := ParseJUnit(strings.NewReader(string(data)), t.Dir)
	if perr != nil {
		return Report{Err: "pytest: unreadable report: " + perr.Error()}
	}
	if rep.Passed+rep.Failed+rep.Skipped == 0 {
		rep.Err = "no tests ran"
	}
	return rep
}

type junitDoc struct {
	Suites []junitSuite `xml:"testsuite"`
	Cases  []junitCase  `xml:"testcase"` // a bare <testsuite> root
	Time   float64      `xml:"time,attr"`
}

type junitSuite struct {
	Cases []junitCase `xml:"testcase"`
	Time  float64     `xml:"time,attr"`
}

type junitCase struct {
	Class   string    `xml:"classname,attr"`
	Name    string    `xml:"name,attr"`
	File    string    `xml:"file,attr"`
	Line    int       `xml:"line,attr"`
	Failure *junitMsg `xml:"failure"`
	Error   *junitMsg `xml:"error"`
	Skipped *junitMsg `xml:"skipped"`
}

type junitMsg struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// pyFrame matches a traceback location line: `tests/test_x.py:12: in f`.
var pyFrame = regexp.MustCompile(`^\s*([^\s:]+\.py):(\d+):`)

// ParseJUnit turns a JUnit XML document (pytest's --junitxml) into a
// Report. root resolves file names to absolute paths.
func ParseJUnit(r io.Reader, root string) (Report, error) {
	var doc junitDoc
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return Report{}, err
	}
	cases, secs := doc.Cases, doc.Time
	for _, s := range doc.Suites {
		cases = append(cases, s.Cases...)
		if doc.Time == 0 {
			secs += s.Time
		}
	}
	rep := Report{Elapsed: time.Duration(secs * float64(time.Second))}
	for _, c := range cases {
		bad := c.Failure
		if bad == nil {
			bad = c.Error
		}
		switch {
		case bad != nil:
			rep.Failed++
			rep.Failures = append(rep.Failures, pyFailure(c, bad, root))
		case c.Skipped != nil:
			rep.Skipped++
		default:
			rep.Passed++
		}
	}
	return rep, nil
}

func pyFailure(c junitCase, bad *junitMsg, root string) Failure {
	file := c.File
	if file == "" {
		file = fileFromClass(root, c.Class)
	}
	f := Failure{Package: file, Test: pyTestName(c, file)}
	if f.Package == "" {
		f.Package = c.Class
	}
	for _, l := range strings.Split(strings.TrimRight(bad.Text, "\n"), "\n") {
		f.Output = append(f.Output, strings.TrimRight(l, "\r"))
	}
	// Prefer the traceback frame in the test's own file; else the first.
	var first, own *[2]string
	for _, l := range f.Output {
		m := pyFrame.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		mm := [2]string{m[1], m[2]}
		if first == nil {
			first = &mm
		}
		if own == nil && file != "" && sameFile(m[1], file) {
			own = &mm
		}
	}
	hit := own
	if hit == nil {
		hit = first
	}
	switch {
	case hit != nil:
		f.File = resolveFile(root, "", hit[0])
		f.Line, _ = strconv.Atoi(hit[1])
	case file != "":
		f.File = resolveFile(root, "", file)
		if c.Line > 0 {
			f.Line = c.Line + 1 // pytest's line attribute is 0-based
		}
	}
	f.Message = strings.TrimSpace(firstLine(bad.Message, ""))
	if f.Message == "" {
		f.Message = firstUseful(f.Output)
	}
	return f
}

func sameFile(a, b string) bool {
	a, b = filepath.ToSlash(a), filepath.ToSlash(b)
	return a == b || strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a)
}

// pyTestName is `Class::test_name[param]` (module part dropped).
func pyTestName(c junitCase, file string) string {
	cls := c.Class
	if mod := strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(file), ".py"), "/", "."); mod != "" {
		if cls == mod {
			cls = ""
		} else {
			cls = strings.TrimPrefix(cls, mod+".")
		}
	}
	if cls == "" {
		return c.Name
	}
	return strings.ReplaceAll(cls, ".", "::") + "::" + c.Name
}

// fileFromClass maps `pkg.test_mod.TestC` to the longest existing
// `pkg/test_mod.py` under root.
func fileFromClass(root, class string) string {
	parts := strings.Split(class, ".")
	for k := len(parts); k >= 1; k-- {
		rel := filepath.Join(parts[:k]...) + ".py"
		if exists(filepath.Join(root, rel)) {
			return rel
		}
	}
	return ""
}
