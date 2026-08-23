package toolchain

import (
	"os"
	"strings"
	"testing"
)

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"="), true
		}
	}
	return "", false
}

func TestEnvPrependsShimDirToExistingPath(t *testing.T) {
	m := New("/data/dhi/toolchain")
	env := m.Env([]string{"HOME=/home/x", "PATH=/usr/bin:/bin", "TERM=dumb"})

	got, ok := envValue(env, "PATH")
	if !ok {
		t.Fatal("PATH lost")
	}
	want := "/data/dhi/toolchain/bin:/usr/bin:/bin"
	if got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
	if len(env) != 3 || !strings.Contains(strings.Join(env, "\n"), "HOME=/home/x") {
		t.Errorf("other vars disturbed: %v", env)
	}
}

func TestEnvIdempotent(t *testing.T) {
	m := New("/data/dhi/toolchain")
	first := m.Env([]string{"PATH=/usr/bin"})
	second := m.Env(first)
	if len(second) != 1 || second[0] != first[0] {
		t.Errorf("not idempotent: %v → %v", first, second)
	}
}

func TestEnvNilInheritsHost(t *testing.T) {
	t.Setenv("PATH", "/host/bin")
	m := New("/data/dhi/toolchain")
	env := m.Env(nil)

	got, ok := envValue(env, "PATH")
	if !ok {
		t.Fatal("PATH missing for inherited env")
	}
	if !strings.HasPrefix(got, "/data/dhi/toolchain/bin:/host/bin") {
		t.Errorf("PATH = %q", got)
	}
	if _, ok := envValue(env, "HOME"); !ok && os.Getenv("HOME") != "" {
		t.Error("inherited environ dropped other variables")
	}
}

func TestEnvExplicitEmptyStaysIsolated(t *testing.T) {
	t.Setenv("PATH", "/host/bin")
	m := New("/data/dhi/toolchain")
	env := m.Env([]string{})

	if len(env) != 1 {
		t.Fatalf("env = %v, want exactly one entry", env)
	}
	got, _ := envValue(env, "PATH")
	if got != "/data/dhi/toolchain/bin" {
		t.Errorf("PATH = %q, want shim-only", got)
	}
}

func TestEnvMissingPathVar(t *testing.T) {
	t.Setenv("PATH", "")
	m := New("/data/dhi/toolchain")
	env := m.Env([]string{"FOO=bar"})

	got, ok := envValue(env, "PATH")
	if !ok || got != "/data/dhi/toolchain/bin" {
		t.Errorf("PATH = %q (%v), want shim-only", got, env)
	}
}
