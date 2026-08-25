package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drjzlyan/dhi/internal/toolchain"
)

// fakeGitShim installs a stub git at root/bin/git printing the given
// --version line, mirroring the shim layout toolchain.Manager expects.
func fakeGitShim(t *testing.T, root, version string) {
	t.Helper()
	shimDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"git version " + version + "\"; fi\n"
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestGitChecksSilentWithoutPin(t *testing.T) {
	// Pre-pin the registry has no git entry and the suite stays silent;
	// once the pin flips (ADR-0009 release), warn-until-installed takes
	// over and this absence-case is obsolete.
	mf, err := toolchain.Embedded()
	if err != nil {
		t.Fatalf("embedded registry: %v", err)
	}
	if _, pinned := mf.Tools["git"]; pinned {
		t.Skip("git pin flipped; silence-case no longer applicable")
	}
	if checks := Git(t.TempDir()); checks != nil {
		t.Errorf("expected no checks pre-pin, got %+v", checks)
	}
}

func TestGitChecksBranches(t *testing.T) {
	const pin = "9.9.9"

	t.Run("absent prefix warns", func(t *testing.T) {
		checks := gitChecks(pin, filepath.Join(t.TempDir(), "absent"))
		c, ok := statusOf(checks, "git/shim")
		if !ok || c.Status != Warn || !strings.Contains(c.Detail, "pending first bootstrap") {
			t.Errorf("absent prefix = %+v (found=%v)", c, ok)
		}
	})

	t.Run("unlocked warns", func(t *testing.T) {
		root := t.TempDir()
		checks := gitChecks(pin, root)
		c, ok := statusOf(checks, "git/shim")
		if !ok || c.Status != Warn || !strings.Contains(c.Detail, "not installed yet") {
			t.Errorf("unlocked = %+v (found=%v)", c, ok)
		}
	})

	t.Run("stale lockfile warns", func(t *testing.T) {
		root := t.TempDir()
		writeLockfile(t, root, map[string]lockedTool{
			"git": {Version: "2.54.0", SHA256: strings.Repeat("a", 64), Path: "tools/git/2.54.0"},
		})
		checks := gitChecks(pin, root)
		c, ok := statusOf(checks, "git/shim")
		if !ok || c.Status != Warn || !strings.Contains(c.Detail, "bootstrap will upgrade") {
			t.Errorf("stale lock = %+v (found=%v)", c, ok)
		}
	})

	t.Run("missing shim fails", func(t *testing.T) {
		root := t.TempDir()
		writeLockfile(t, root, map[string]lockedTool{
			"git": {Version: pin, SHA256: strings.Repeat("b", 64), Path: "tools/git/" + pin},
		})
		checks := gitChecks(pin, root)
		c, ok := statusOf(checks, "git/shim")
		if !ok || c.Status != Fail || !strings.Contains(c.Detail, "shim missing") {
			t.Errorf("missing shim = %+v (found=%v)", c, ok)
		}
	})

	t.Run("version mismatch fails", func(t *testing.T) {
		root := t.TempDir()
		writeLockfile(t, root, map[string]lockedTool{
			"git": {Version: pin, SHA256: strings.Repeat("b", 64), Path: "tools/git/" + pin},
		})
		fakeGitShim(t, root, "2.55.0-somethingelse")
		checks := gitChecks(pin, root)
		c, ok := statusOf(checks, "git/version")
		if !ok || c.Status != Fail || !strings.Contains(c.Detail, "registry pins") {
			t.Errorf("mismatch = %+v (found=%v)", c, ok)
		}
	})

	t.Run("healthy install passes", func(t *testing.T) {
		root := t.TempDir()
		writeLockfile(t, root, map[string]lockedTool{
			"git": {Version: pin, SHA256: strings.Repeat("c", 64), Path: "tools/git/" + pin},
		})
		fakeGitShim(t, root, pin)
		checks := gitChecks(pin, root)
		if len(checks) != 1 {
			t.Fatalf("healthy install emitted %+v, want single OK check", checks)
		}
		if checks[0].Name != "git/version" || checks[0].Status != OK {
			t.Errorf("healthy = %+v", checks[0])
		}
	})
}
