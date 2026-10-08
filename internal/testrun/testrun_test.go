package testrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const stream = `{"Action":"run","Package":"ex/pkg","Test":"TestOK"}
{"Action":"output","Package":"ex/pkg","Test":"TestOK","Output":"--- PASS: TestOK (0.00s)\n"}
{"Action":"pass","Package":"ex/pkg","Test":"TestOK","Elapsed":0}
{"Action":"run","Package":"ex/pkg","Test":"TestBad"}
{"Action":"output","Package":"ex/pkg","Test":"TestBad","Output":"=== RUN   TestBad\n"}
{"Action":"output","Package":"ex/pkg","Test":"TestBad","Output":"    bad_test.go:12: got 1, want 2\n"}
{"Action":"output","Package":"ex/pkg","Test":"TestBad","Output":"--- FAIL: TestBad (0.00s)\n"}
{"Action":"fail","Package":"ex/pkg","Test":"TestBad","Elapsed":0}
{"Action":"run","Package":"ex/pkg","Test":"TestSkip"}
{"Action":"skip","Package":"ex/pkg","Test":"TestSkip","Elapsed":0}
{"Action":"fail","Package":"ex/pkg","Elapsed":0.5}
{"Action":"output","Package":"ex/broken","Output":"# ex/broken\n"}
{"Action":"output","Package":"ex/broken","Output":"./broken.go:7:2: undefined: nope\n"}
{"Action":"fail","Package":"ex/broken","Elapsed":0}
not json at all
`

func TestParseCountsAndLocatesFailures(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pkg/bad_test.go", "broken.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rep := Parse(strings.NewReader(stream), root)
	if rep.Passed != 1 || rep.Skipped != 1 || rep.Failed != 2 {
		t.Fatalf("counts = %+v", rep)
	}
	if len(rep.Failures) != 2 {
		t.Fatalf("failures = %+v", rep.Failures)
	}
	// Sorted by package: ex/broken (build) then ex/pkg.
	b, p := rep.Failures[0], rep.Failures[1]
	if b.Test != "" || b.Line != 7 || b.Message != "undefined: nope" || b.File != filepath.Join(root, "broken.go") {
		t.Fatalf("build failure = %+v", b)
	}
	if p.Test != "TestBad" || p.Line != 12 || p.Message != "got 1, want 2" || p.File != filepath.Join(root, "pkg", "bad_test.go") {
		t.Fatalf("test failure = %+v", p)
	}
}

func TestParsePackageFailWithFailingTestsAddsNoExtra(t *testing.T) {
	in := `{"Action":"fail","Package":"p","Test":"TestA","Elapsed":0}
{"Action":"fail","Package":"p","Elapsed":0}`
	rep := Parse(strings.NewReader(in), t.TempDir())
	if rep.Failed != 1 || len(rep.Failures) != 1 {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestLookPathUsesSuppliedEnvOnly(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "go")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := LookPath("go", []string{"HOME=/x", "PATH=/nonexistent:" + dir})
	if err != nil || got != bin {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := LookPath("go", []string{"PATH=/nonexistent"}); err == nil {
		t.Fatal("must not fall back to the host PATH")
	}
}

func TestRunWithStubGo(t *testing.T) {
	dir := t.TempDir()
	stub := "#!/bin/sh\n" +
		`printf '%s\n' '{"Action":"output","Package":"p","Test":"TestX","Output":"    x_test.go:3: boom\n"}'` + "\n" +
		`printf '%s\n' '{"Action":"fail","Package":"p","Test":"TestX","Elapsed":0}'` + "\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	rep := Run(context.Background(), t.TempDir(), []string{"PATH=" + dir}, "./...")
	if rep.Err != "" || rep.Failed != 1 || rep.Failures[0].Message != "boom" {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestRunReportsMissingGoAndEmptyOutput(t *testing.T) {
	if rep := Run(context.Background(), t.TempDir(), []string{"PATH=/nonexistent"}, "."); !strings.Contains(rep.Err, "not found") {
		t.Fatalf("rep = %+v", rep)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte("#!/bin/sh\necho 'cannot find module' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rep := Run(context.Background(), t.TempDir(), []string{"PATH=" + dir}, ".")
	if !strings.Contains(rep.Err, "cannot find module") {
		t.Fatalf("rep = %+v", rep)
	}
}
