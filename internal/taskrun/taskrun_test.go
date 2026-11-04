package taskrun

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(ts []Task) []string {
	var o []string
	for _, t := range ts {
		o = append(o, t.Runner+":"+t.Name)
	}
	return o
}

func TestPackageScriptsKeepOrderAndPickManagerByLockfile(t *testing.T) {
	d := t.TempDir()
	write(t, d, "package.json", `{"name":"x","scripts":{"zeta":"tsc","alpha":"vite"},"dependencies":{"a":"1"}}`)
	ts, notes := Discover(d, nil)
	if len(notes) != 0 || !reflect.DeepEqual(names(ts), []string{"npm:zeta", "npm:alpha"}) {
		t.Fatalf("npm: %v %v", names(ts), notes)
	}
	if ts[0].Command() != "npm run zeta" || ts[0].Hint != "tsc" {
		t.Errorf("task = %+v", ts[0])
	}
	write(t, d, "pnpm-lock.yaml", "")
	ts, _ = Discover(d, nil)
	if ts[0].Command() != "pnpm run zeta" {
		t.Errorf("pnpm: %s", ts[0].Command())
	}
	_ = os.Remove(filepath.Join(d, "pnpm-lock.yaml"))
	write(t, d, "yarn.lock", "")
	ts, _ = Discover(d, nil)
	if ts[0].Runner != "yarn" {
		t.Errorf("yarn: %s", ts[0].Runner)
	}
}

func TestBadPackageJSONIsReported(t *testing.T) {
	d := t.TempDir()
	write(t, d, "package.json", `{"scripts": {"a": 5}}`)
	ts, notes := Discover(d, nil)
	if len(ts) != 0 || len(notes) != 1 {
		t.Fatalf("tasks=%v notes=%v", ts, notes)
	}
}

func TestMakeTargetsSkipSpecialPatternAndAssignments(t *testing.T) {
	d := t.TempDir()
	write(t, d, "Makefile", `CC := gcc
VAR = x
.PHONY: build test
build: dep ## compile it
	go build
%.o: %.c
	$(CC) -c $<
test lint:
	go test
$(OUT): x
ifdef Y
endif
verify:: build
build: again
`)
	ts, _ := Discover(d, nil)
	if want := []string{"make:build", "make:test", "make:lint", "make:verify"}; !reflect.DeepEqual(names(ts), want) {
		t.Fatalf("got %v want %v", names(ts), want)
	}
	if ts[0].Hint != "compile it" || ts[0].Command() != "make build" {
		t.Errorf("task = %+v", ts[0])
	}
}

func TestJustParsedWithoutJustAndFromList(t *testing.T) {
	d := t.TempDir()
	write(t, d, "justfile", "name := \"x\"\n\n# build it\nbuild target='a':\n  echo\n\n@test: build\n  echo\n")
	ts, _ := Discover(d, nil)
	if want := []string{"just:build", "just:test"}; !reflect.DeepEqual(names(ts), want) {
		t.Fatalf("parse: %v", names(ts))
	}
	if ts[0].Hint != "build it" {
		t.Errorf("hint = %q", ts[0].Hint)
	}
	list := func(string) (string, error) {
		return "Available recipes:\n    zed # last\n    alpha *args\n    [group]\n", nil
	}
	ts, _ = Discover(d, list)
	if want := []string{"just:zed", "just:alpha"}; !reflect.DeepEqual(names(ts), want) || ts[0].Hint != "last" {
		t.Fatalf("list: %v %+v", names(ts), ts)
	}
	_, notes := Discover(d, func(string) (string, error) { return "", os.ErrPermission })
	if len(notes) != 1 {
		t.Errorf("a failing just --list must be reported, got %v", notes)
	}
}

func TestParseProblems(t *testing.T) {
	d := t.TempDir()
	for _, f := range []string{"main.go", "a.c", "app.ts", "x.js"} {
		write(t, d, f, "")
	}
	out := "building…\r\n" +
		"./main.go:12:5: undefined: foo\r\n" +
		"a.c:3:9: warning: unused variable 'x'\n" +
		"  " + filepath.Join(d, "x.js") + ":7:1: Unexpected console statement [Warning/no-console]\n" +
		"app.ts(4,2): error TS2304: Cannot find name 'q'.\n" +
		"main.go:30: plain log line\n" +
		"gone.go:1:1: missing file\n" +
		"12:30:45: not a file\n" +
		"http://localhost:3000: up\n" +
		"./main.go:12:5: undefined: foo\n"
	ps := ParseProblems(out, d)
	want := []Problem{
		{filepath.Join(d, "main.go"), 12, 5, 1, "undefined: foo"},
		{filepath.Join(d, "a.c"), 3, 9, 2, "unused variable 'x'"},
		{filepath.Join(d, "x.js"), 7, 1, 1, "Unexpected console statement [Warning/no-console]"},
		{filepath.Join(d, "app.ts"), 4, 2, 1, "TS2304: Cannot find name 'q'."},
		{filepath.Join(d, "main.go"), 30, 0, 1, "plain log line"},
	}
	if len(ps) != len(want) {
		t.Fatalf("got %d: %+v", len(ps), ps)
	}
	for i := range want {
		if ps[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, ps[i], want[i])
		}
	}
}
