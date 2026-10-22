package hostenv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkipsRCNoise(t *testing.T) {
	out := []byte("welcome to zsh!\nnvm loaded\n" + marker + "PATH=/a:/b\x00HOME=/h\x00MULTI=line1\nline2\x00_=/usr/bin/env\x00")
	env, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := Lookup(env, "PATH"); v != "/a:/b" {
		t.Fatalf("PATH = %q", v)
	}
	if v, _ := Lookup(env, "MULTI"); v != "line1\nline2" {
		t.Fatalf("multi-line value = %q", v)
	}
	if _, ok := Lookup(env, "_"); ok {
		t.Fatal("shell bookkeeping vars are dropped")
	}
	if _, err := Parse([]byte("no marker here")); err == nil {
		t.Fatal("missing marker must fail")
	}
}

func TestResolveFallsBackAndNamesWhy(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	probe = func(string) ([]string, error) { return nil, errors.New("the login shell took more than 3s") }
	defer func() { probe = probeShell }()
	s := Reload()
	if s.Source != "DHI's own environment" || s.Err == nil || !strings.Contains(s.Err.Error(), "3s") {
		t.Fatalf("snapshot = %+v", s)
	}
}

// A tool only on the login PATH (nvm, Homebrew) is found; DHI's own PATH
// does not have it.
func TestLookPathUsesTheLoginPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "only-in-login")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("SHELL", "/bin/sh")
	probe = func(string) ([]string, error) { return []string{"PATH=" + dir, "FOO=bar"}, nil }
	defer func() { probe = probeShell }()
	s := Reload()
	if s.Source != "login shell" {
		t.Fatalf("source = %q", s.Source)
	}
	if p, err := LookPath("only-in-login"); err != nil || p != bin {
		t.Fatalf("LookPath = %q %v", p, err)
	}
	if Get("FOO") != "bar" {
		t.Fatal("login variables reach the env")
	}
	if Get("HOME") == "" {
		t.Fatal("DHI's own variables the shell did not set are kept")
	}
}

// The real probe works with a POSIX shell (no rc files needed).
func TestProbeRealShell(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	env, err := probeShell("/bin/sh")
	if err != nil {
		t.Skipf("sh -ilc unsupported here: %v", err)
	}
	if _, ok := Lookup(env, "PATH"); !ok {
		t.Fatalf("no PATH in %v", env)
	}
}

func TestWith(t *testing.T) {
	env := With([]string{"A=1", "B=2"}, "A", "3")
	if v, _ := Lookup(env, "A"); v != "3" || len(env) != 2 {
		t.Fatalf("%v", env)
	}
}

func TestApplySetsTheProcessEnv(t *testing.T) {
	t.Setenv("DHI_APPLY_TEST", "old")
	Set(Snapshot{Env: []string{"DHI_APPLY_TEST=new"}, Source: "login shell"})
	defer Set(Snapshot{Env: os.Environ()})
	Apply()
	if os.Getenv("DHI_APPLY_TEST") != "new" {
		t.Fatal("Apply must set the process env")
	}
}
