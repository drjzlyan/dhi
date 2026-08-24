package toolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitEnvPrependsShimAndHardens(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	env := m.GitEnv(nil)
	if got := firstPathEntry(valueOf(env, "PATH")); got != m.ShimDir() {
		t.Errorf("PATH first entry %q, want shim dir %q", got, m.ShimDir())
	}
	if v := valueOf(env, "GIT_CONFIG_NOSYSTEM"); v != "1" {
		t.Errorf("GIT_CONFIG_NOSYSTEM=%q, want 1", v)
	}
	if v := valueOf(env, "GIT_TERMINAL_PROMPT"); v != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT=%q, want 0", v)
	}
	wantGlobal := filepath.Join(root, gitCfgRel)
	if v := valueOf(env, "GIT_CONFIG_GLOBAL"); v != wantGlobal {
		t.Errorf("GIT_CONFIG_GLOBAL=%q, want %q", v, wantGlobal)
	}
	if n := countKey(env, "GIT_CONFIG_GLOBAL"); n != 1 {
		t.Errorf("GIT_CONFIG_GLOBAL appears %d times, want 1", n)
	}
}

func TestGitEnvEmptyBaseStaysHostFree(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "")
	t.Setenv("DHI_PROBE", "leak")
	m := New(t.TempDir())
	env := m.GitEnv([]string{})
	if valueOf(env, "DHI_PROBE") != "" {
		t.Error("explicitly empty base leaked a host var")
	}
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "PATH="):
			if kv != "PATH="+m.ShimDir() {
				t.Errorf("empty base PATH = %q, want shim-only", kv)
			}
		case strings.HasPrefix(kv, "GIT_"):
			continue // hardening vars are expected
		default:
			t.Errorf("unexpected entry in isolated env: %q", kv)
		}
	}
}

func TestGitEnvIdempotentAndReplacesManagedKeys(t *testing.T) {
	m := New(t.TempDir())
	base := []string{
		"GIT_CONFIG_GLOBAL=/host/leaky/config",
		"GIT_TERMINAL_PROMPT=1",
		"HOME=/home/user",
	}
	once := m.GitEnv(base)
	twice := m.GitEnv(once)
	if got := valueOf(twice, "GIT_CONFIG_GLOBAL"); got == "/host/leaky/config" {
		t.Error("user GIT_CONFIG_GLOBAL survived GitEnv")
	}
	for _, k := range managedGitVars {
		if n := countKey(twice, k); n != 1 {
			t.Errorf("%s appears %d times after double application, want 1", k, n)
		}
	}
	if valueOf(twice, "HOME") != "/home/user" {
		t.Error("unmanaged key was dropped")
	}
}

func TestEnsureGitConfigCreatesDeterministicLayout(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	if err := m.EnsureGitConfig(); err != nil {
		t.Fatalf("first EnsureGitConfig: %v", err)
	}
	cfgPath := filepath.Join(root, gitCfgRel)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("managed config missing: %v", err)
	}
	cfg := string(data)
	if !strings.Contains(cfg, "defaultBranch = main") {
		t.Error("config missing deterministic defaultBranch")
	}
	hooks := filepath.Join(root, gitHooksRel)
	info, err := os.Stat(hooks)
	if err != nil || !info.IsDir() {
		t.Fatalf("hooks dir missing: %v", err)
	}
	if !strings.Contains(cfg, filepath.Join(hooks, "")) {
		t.Errorf("config hooksPath %q does not point at managed hooks dir", cfg)
	}
	// Idempotent: second run must not error or rewrite.
	before, _ := os.ReadFile(cfgPath)
	if err := m.EnsureGitConfig(); err != nil {
		t.Fatalf("second EnsureGitConfig: %v", err)
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Error("existing managed config was rewritten")
	}
}

func TestGitBinIsShimPath(t *testing.T) {
	m := New(t.TempDir())
	if want := filepath.Join(m.ShimDir(), "git"); m.GitBin() != want {
		t.Errorf("GitBin=%q, want %q", m.GitBin(), want)
	}
}

func valueOf(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

func countKey(env []string, key string) int {
	n := 0
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 && kv[:i] == key {
			n++
		}
	}
	return n
}
