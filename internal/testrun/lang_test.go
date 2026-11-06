package testrun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const junitFixture = `<?xml version="1.0" encoding="utf-8"?>
<testsuites><testsuite name="pytest" errors="1" failures="1" skipped="1" tests="5" time="0.120">
<testcase classname="tests.test_calc" name="test_ok" file="tests/test_calc.py" line="3" time="0.001"/>
<testcase classname="tests.test_calc.TestAdd" name="test_bad[2-3]" file="tests/test_calc.py" line="9" time="0.002">
<failure message="assert 5 == 6&#10; +  where 5 = add(2, 3)">self = &lt;tests.test_calc.TestAdd object&gt;
tests/test_calc.py:11: in test_bad
    assert add(2, 3) == 6
src/calc.py:4: in add
    return a + b
E   assert 5 == 6</failure></testcase>
<testcase classname="tests.test_calc" name="test_skip" file="tests/test_calc.py" line="20" time="0"><skipped type="pytest.skip" message="later">skipped</skipped></testcase>
<testcase classname="tests.test_broken" name="tests.test_broken" file="" line="0" time="0"><error message="collection failure">tests/test_broken.py:2: in &lt;module&gt;
    import nope
E   ModuleNotFoundError: No module named 'nope'</error></testcase>
<testcase classname="tests.test_calc" name="test_fine" file="tests/test_calc.py" line="30" time="0"/>
</testsuite></testsuites>`

func TestParseJUnitNormalisesFailures(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"tests/test_calc.py", "tests/test_broken.py", "src/calc.py"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755)
		_ = os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0o644)
	}
	rep, err := ParseJUnit(strings.NewReader(junitFixture), root)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 2 || rep.Failed != 2 || rep.Skipped != 1 || len(rep.Failures) != 2 {
		t.Fatalf("counts = %+v", rep)
	}
	bad, broken := rep.Failures[0], rep.Failures[1]
	// The jump lands on the frame in the test's own file, not the library's.
	if bad.Test != "TestAdd::test_bad[2-3]" || bad.Package != "tests/test_calc.py" ||
		bad.File != filepath.Join(root, "tests", "test_calc.py") || bad.Line != 11 ||
		bad.Message != "assert 5 == 6" || len(bad.Output) == 0 {
		t.Fatalf("failure = %+v", bad)
	}
	// A collection error has no file attribute: the class path resolves it.
	if broken.File != filepath.Join(root, "tests", "test_broken.py") || broken.Line != 2 ||
		!strings.HasPrefix(broken.Message, "collection failure") {
		t.Fatalf("collection error = %+v", broken)
	}
}

func TestParseJUnitBareTestsuiteRoot(t *testing.T) {
	rep, err := ParseJUnit(strings.NewReader(`<testsuite tests="1"><testcase classname="m" name="test_a"/></testsuite>`), t.TempDir())
	if err != nil || rep.Passed != 1 {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
}

// stubBin writes an executable shell script named name and returns a PATH env.
func stubBin(t *testing.T, name, script string) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir + ":/usr/bin:/bin"}
}

func TestRunPytestWithFakeRunner(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "test_x.py"), []byte("x\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "args.txt"), nil, 0o644)
	// The fake pytest honours --junitxml like the real one and records argv.
	script := `for a in "$@"; do case "$a" in --junitxml=*) out="${a#--junitxml=}";; esac; done
printf '%s\n' "$@" > args.txt
cat > "$out" <<'XML'
<testsuites><testsuite time="0.5"><testcase classname="test_x" name="test_a" file="test_x.py" line="0"><failure message="boom">test_x.py:7: AssertionError</failure></testcase></testsuite></testsuites>
XML
exit 1
`
	env := stubBin(t, "pytest", script)
	l, ok := ForPath("/w/test_x.py")
	if !ok || l.Name != "Python" {
		t.Fatalf("ForPath = %+v %v", l, ok)
	}
	rep := l.Run(context.Background(), env, Target{Dir: root, File: filepath.Join(root, "test_x.py"), Pattern: "a"})
	if rep.Err != "" || rep.Failed != 1 || rep.Failures[0].Line != 7 || rep.Failures[0].Message != "boom" {
		t.Fatalf("rep = %+v", rep)
	}
	args, _ := os.ReadFile(filepath.Join(root, "args.txt"))
	for _, want := range []string{"-k\na\n", filepath.Join(root, "test_x.py")} {
		if !strings.Contains(string(args), want) {
			t.Errorf("argv missing %q:\n%s", want, args)
		}
	}
	// The temp report is gone afterwards.
	var tmp string
	for _, a := range strings.Split(string(args), "\n") {
		if strings.HasPrefix(a, "--junitxml=") {
			tmp = strings.TrimPrefix(a, "--junitxml=")
		}
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("temp junit file %s left behind", tmp)
	}
}

func TestRunPytestMissingNamesTheTool(t *testing.T) {
	rep := runPytest(context.Background(), []string{"PATH=" + t.TempDir()}, Target{Dir: t.TempDir(), File: "x.py"})
	if rep.MissingTool != "pytest" || !strings.Contains(rep.Err, "pytest was not found in .venv/bin, then venv/bin, then your PATH") {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestRunPytestNoReportSurfacesStderr(t *testing.T) {
	env := stubBin(t, "pytest", "echo 'ERROR: usage: pytest [options]' >&2\nexit 4\n")
	rep := runPytest(context.Background(), env, Target{Dir: t.TempDir(), File: "x.py"})
	if !strings.Contains(rep.Err, "usage: pytest") || rep.MissingTool != "" {
		t.Fatalf("rep = %+v", rep)
	}
}

const jestFixture = `Determining test suites to run...
{"numTotalTests":3,"testResults":[
{"name":"/w/src/a.test.ts","status":"failed","message":"","assertionResults":[
 {"fullName":"calc adds","title":"adds","ancestorTitles":["calc"],"status":"passed","failureMessages":[]},
 {"fullName":"calc subtracts","title":"subtracts","ancestorTitles":["calc"],"status":"failed",
  "failureMessages":["Error: \u001b[31mexpect(received).toBe(expected)\u001b[39m\n\n    at Object.<anonymous> (/w/src/a.test.ts:9:21)\n    at node_modules/jest/x.js:1:1"],
  "location":{"line":9,"column":21}},
 {"fullName":"calc later","title":"later","ancestorTitles":["calc"],"status":"todo","failureMessages":[]}]},
{"name":"/w/src/broken.test.ts","status":"failed","message":"SyntaxError: Unexpected token\n    at /w/src/broken.test.ts:3:1","assertionResults":[]}
]}`

func TestParseJSNormalisesJestAndVitest(t *testing.T) {
	rep, ok := ParseJS([]byte(jestFixture), "/w")
	if !ok {
		t.Fatal("no report found")
	}
	if rep.Passed != 1 || rep.Failed != 2 || rep.Skipped != 1 || len(rep.Failures) != 2 {
		t.Fatalf("counts = %+v", rep)
	}
	f, b := rep.Failures[0], rep.Failures[1]
	if f.Test != "calc subtracts" || f.Package != "src/a.test.ts" || f.File != "/w/src/a.test.ts" || f.Line != 9 ||
		f.Message != "Error: expect(received).toBe(expected)" {
		t.Fatalf("failure = %+v", f)
	}
	if b.Test != "" || b.Line != 3 || b.File != "/w/src/broken.test.ts" || !strings.HasPrefix(b.Message, "SyntaxError") {
		t.Fatalf("suite failure = %+v", b)
	}
	if _, ok := ParseJS([]byte("no json here"), "/w"); ok {
		t.Fatal("garbage parsed as a report")
	}
}

func TestRunJSPrefersProjectBinThenNamesMissing(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "node_modules", ".bin")
	_ = os.MkdirAll(bin, 0o755)
	script := "#!/bin/sh\ncat <<'JSON'\n" + jestFixture + "\nJSON\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "vitest"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l, _ := ForPath("a.test.ts")
	rep := l.Run(context.Background(), []string{"PATH=/usr/bin:/bin"}, Target{Dir: root, File: filepath.Join(root, "a.test.ts")})
	if rep.Err != "" || rep.Failed != 2 {
		t.Fatalf("rep = %+v", rep)
	}
	none := l.Run(context.Background(), []string{"PATH=" + t.TempDir()}, Target{Dir: t.TempDir(), File: "a.test.ts"})
	if none.MissingTool != "vitest" || none.Err == "" {
		t.Fatalf("missing = %+v", none)
	}
}

func TestForPathTable(t *testing.T) {
	for path, want := range map[string]string{"a.go": "Go", "a.py": "Python", "a.tsx": "JavaScript", "a.rs": ""} {
		l, ok := ForPath(path)
		if (want == "") == ok || l.Name != want {
			t.Errorf("ForPath(%s) = %q %v", path, l.Name, ok)
		}
	}
}

// TestLivePytest runs a real pytest when the user has one (DHI_LIVE_PY=1).
func TestLivePytest(t *testing.T) {
	if os.Getenv("DHI_LIVE_PY") == "" {
		t.Skip("set DHI_LIVE_PY=1 to run against a real pytest")
	}
	if _, err := exec.LookPath("pytest"); err != nil {
		t.Skip("pytest is not on PATH")
	}
	root := t.TempDir()
	src := "def test_ok():\n    assert 1 + 1 == 2\n\n\nclass TestC:\n    def test_bad(self):\n        x = 2\n        assert x == 3\n"
	file := filepath.Join(root, "test_live.py")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	l, _ := ForPath(file)
	rep := l.Run(context.Background(), os.Environ(), Target{Dir: root, File: file})
	if rep.Err != "" || rep.Passed != 1 || rep.Failed != 1 || len(rep.Failures) != 1 {
		t.Fatalf("rep = %+v", rep)
	}
	f := rep.Failures[0]
	t.Logf("failure: %+v", f)
	if f.Test != "TestC::test_bad" || f.Line != 8 || filepath.Base(f.File) != "test_live.py" {
		t.Fatalf("failure = %+v", f)
	}
}

func TestPytestPrefersProjectVenvOverPath(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "test_x.py"), []byte("x\n"), 0o644)
	xmlOut := `<testsuite><testcase classname="test_x" name="%s"/></testsuite>`
	mk := func(dir, name string) {
		_ = os.MkdirAll(dir, 0o755)
		script := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in --junitxml=*) out=\"${a#--junitxml=}\";; esac; done\n" +
			"cat > \"$out\" <<'XML'\n" + strings.Replace(xmlOut, "%s", name, 1) + "\nXML\n"
		if err := os.WriteFile(filepath.Join(dir, "pytest"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pathDir := t.TempDir()
	mk(pathDir, "from_path")
	env := []string{"PATH=" + pathDir + ":/usr/bin:/bin"}
	tg := Target{Dir: root, File: filepath.Join(root, "test_x.py")}
	// Only PATH has it: PATH wins. Then venv/bin, then .venv/bin shadow it in order.
	for _, c := range []struct{ dir, want string }{{"", "from_path"}, {"venv", "venv"}, {".venv", "dotvenv"}} {
		if c.dir != "" {
			name := c.want
			mk(filepath.Join(root, c.dir, "bin"), name)
		}
		bin, err := LookPython(root, "pytest", "pytest", env)
		if err != nil {
			t.Fatal(err)
		}
		wantDir := pathDir
		if c.dir != "" {
			wantDir = filepath.Join(root, c.dir, "bin")
		}
		if filepath.Dir(bin) != wantDir {
			t.Fatalf("%s: picked %s, want dir %s", c.want, bin, wantDir)
		}
	}
	if rep := runPytest(context.Background(), env, tg); rep.Err != "" || rep.Passed != 1 {
		t.Fatalf("rep = %+v", rep)
	}
}
