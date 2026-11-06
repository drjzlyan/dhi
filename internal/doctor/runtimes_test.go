package doctor

import (
	"github.com/drjzlyan/dhi/internal/agentkit/clirun"
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
	if !found || got["runtime/claude"] != Fail || !strings.Contains(claudeCheck.Detail, "is not on your PATH") {
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
	// F-066: the resume row says whether turns continue the session.
	if c, ok := statusOf(checks, "runtime/claude/resume"); !ok || c.Status != OK || !strings.Contains(c.Detail, "verified") {
		t.Errorf("claude resume row = %+v", c)
	}

	// F-123: the web row says whether the CLI's own web tools can be switched off.
	if c, ok := statusOf(checks, "runtime/claude/web"); !ok || c.Status != OK || !strings.Contains(c.Detail, "switched off") {
		t.Errorf("claude web row = %+v", c)
	}

	// G-A4: the prompt-size row says what a prompt over the argv budget does.
	if c, ok := statusOf(checks, "runtime/claude/prompt-size"); !ok || c.Status != OK || !strings.Contains(c.Detail, "stdin") {
		t.Errorf("claude prompt-size row = %+v", c)
	}

	// F-179: claude's hook row says its own tools are covered per run, and
	// that the user's config is not edited.
	if c, ok := statusOf(checks, "runtime/claude/hooks"); !ok || !strings.Contains(c.Detail, "own tools, handed to it for each run only") {
		t.Errorf("claude hooks row = %+v", c)
	}

	// untested version present: strict fail
	bad := t.TempDir()
	writeExecutable(t, bad, "claude", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"9.9.9 (Claude Code)\"; fi\n")
	lookPath = stubLook(bad)
	checks = Runtimes()
	if c, _ := statusOf(checks, "runtime/claude"); c.Status != Fail {
		t.Errorf("untested version = %v, want fail (%+v)", c.Status, checks)
	}
	if c, _ := statusOf(checks, "runtime/claude"); !strings.Contains(c.Detail, "different major") {
		t.Errorf("major-change detail wrong: %s", c.Detail)
	}

	// same major, newer minor: usable, so a warning rather than a failure
	drift := t.TempDir()
	writeExecutable(t, drift, "claude", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"2.1.285 (Claude Code)\"; fi\n")
	lookPath = stubLook(drift)
	checks = Runtimes()
	c, _ := statusOf(checks, "runtime/claude")
	if c.Status != Warn || !strings.Contains(c.Detail, "newer than the verified 2.1.177") {
		t.Errorf("same-major drift = %v %q, want a warning", c.Status, c.Detail)
	}
}

// A CLI DHI installed into its managed folder is found when it is not on PATH.

// TestWebRowWarnsForCLIsThatCannotBeRestricted: a CLI without a verified
// switch is named in doctor, never reported as restricted (F-123).
func TestWebRowWarnsForCLIsThatCannotBeRestricted(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	no := clirun.NewRegistry(nil).CannotDenyWeb()
	if len(no) == 0 {
		t.Skip("every adapter can deny native web tools")
	}
	c, _ := clirun.NewRegistry(nil).Get(no[0])
	dir := t.TempDir()
	lookPath = stubLook(dir)
	writeExecutable(t, dir, c.Bin, "#!/bin/sh\necho \""+c.Tested+"\"\n")
	got, ok := statusOf(Runtimes(), "runtime/"+c.Name+"/web")
	if !ok {
		t.Fatalf("no web row for %s", c.Name)
	}
	if got.Status != Warn || !strings.Contains(got.Detail, "no verified way") {
		t.Fatalf("%s web row = %+v", c.Name, got)
	}
}

// TestHooksRowNamesTheCoverageGap: a CLI with no per-run hook input is
// reported as covered at DHI's tools only, with the evidence's reason
// (F-179, ADR-0047).
func TestHooksRowNamesTheCoverageGap(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	c, ok := clirun.NewRegistry(nil).Get("codex")
	if !ok || c.HooksArgs != nil {
		t.Skip("codex takes hooks per run now")
	}
	dir := t.TempDir()
	lookPath = stubLook(dir)
	writeExecutable(t, dir, c.Bin, "#!/bin/sh\necho \""+c.Tested+"\"\n")
	got, found := statusOf(Runtimes(), "runtime/codex/hooks")
	if !found || !strings.Contains(got.Detail, "DHI's tools only") || !strings.Contains(got.Detail, "no verified per-run hook input") {
		t.Fatalf("codex hooks row = %+v", got)
	}
}
