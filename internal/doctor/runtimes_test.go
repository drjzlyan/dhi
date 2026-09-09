package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubLook resolves names against dir like exec.LookPath would against
// a PATH containing dir.
func stubLook(dir string) func(string) (string, error) {
	return func(name string) (string, error) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
		return p, nil
	}
}

func writeExecutable(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

const versionedClaude = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"2.1.177 (Claude Code)\"; fi\n"

func TestRuntimes(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })

	fresh := t.TempDir()
	lookPath = stubLook(fresh)

	// nothing on PATH: every registered CLI is a fail with a named cause
	checks := Runtimes()
	if len(checks) == 0 {
		t.Fatal("Runtimes() with no binaries must still report failures")
	}
	got := map[string]Status{}
	for _, c := range checks {
		got[c.Name] = c.Status
	}
	claudeCheck, found := statusOf(checks, "runtime/claude")
	if !found || got["runtime/claude"] != Fail || !strings.Contains(claudeCheck.Detail, "not found on PATH") {
		t.Errorf("missing claude must fail with a named cause: %+v", checks)
	}

	// pinned version present: OK, plus env pass-through warns when unset
	writeExecutable(t, fresh, "claude", versionedClaude)
	checks = Runtimes()
	if c, _ := statusOf(checks, "runtime/claude"); c.Status != OK {
		t.Errorf("pinned claude = %v, want ok (%+v)", c.Status, checks)
	}
	if c, _ := statusOf(checks, "runtime/claude/anthropic_api_key"); c.Status != Warn {
		t.Errorf("unset ANTHROPIC_API_KEY must warn (%+v)", checks)
	}

	// untested version present: strict fail
	bad := t.TempDir()
	writeExecutable(t, bad, "claude", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"9.9.9 (Claude Code)\"; fi\n")
	lookPath = stubLook(bad)
	checks = Runtimes()
	if c, _ := statusOf(checks, "runtime/claude"); c.Status != Fail {
		t.Errorf("untested version = %v, want fail (%+v)", c.Status, checks)
	}
	if c, _ := statusOf(checks, "runtime/claude"); !strings.Contains(c.Detail, "9.9.9 untested") {
		t.Errorf("untested detail wrong: %s", c.Detail)
	}
}
