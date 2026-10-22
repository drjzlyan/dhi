package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit puts a stub git on a private PATH: --version prints version,
// config --get prints the given identity.
func fakeGit(t *testing.T, version, name, email string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"git version " + version + "\"; exit 0; fi\n"
	if name != "" {
		script += "case \"$*\" in *user.name*) echo " + name + "; exit 0;; esac\n"
	}
	if email != "" {
		script += "case \"$*\" in *user.email*) echo " + email + "; exit 0;; esac\n"
	}
	script += "exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(n string) (string, error) {
		if n == "git" {
			return filepath.Join(dir, "git"), nil
		}
		return "", errors.New("not found")
	}
}

func TestGitReportsTheUsersGit(t *testing.T) {
	fakeGit(t, "2.55.0", "Ada", "ada@example.com")
	if c, ok := statusOf(Git(), "git/version"); !ok || c.Status != OK || !strings.Contains(c.Detail, "2.55.0") {
		t.Fatalf("git = %+v", c)
	}
	if c, ok := statusOf(Identity(), "identity"); !ok || c.Status != OK || !strings.Contains(c.Detail, "Ada <ada@example.com>") {
		t.Fatalf("identity = %+v", c)
	}
}

func TestIdentityUnsetWarnsNamingTheFix(t *testing.T) {
	fakeGit(t, "2.55.0", "", "")
	c, ok := statusOf(Identity(), "identity")
	if !ok || c.Status != Warn || !strings.Contains(c.Detail, "user.name") {
		t.Fatalf("unset = %+v (found=%v)", c, ok)
	}
}

func TestMissingGitAndGHWarnWithTheInstallCommand(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if c, _ := statusOf(Git(), "git/version"); c.Status != Warn || !strings.Contains(c.Detail, "install") {
		t.Fatalf("git = %+v", c)
	}
	if Identity() != nil {
		t.Fatal("identity is silent without git")
	}
	if c, _ := statusOf(GH(), "gh/cli"); c.Status != Warn || !strings.Contains(c.Detail, "gh auth login") {
		t.Fatalf("gh = %+v", c)
	}
}

func TestLegacyToolchainIsNamedWithItsSize(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	if LegacyToolchain() != nil {
		t.Fatal("no old folder: no row")
	}
	dir := filepath.Join(data, "dhi", "toolchain", "tools")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "big"), make([]byte, 3<<20), 0o644)
	c, ok := statusOf(LegacyToolchain(), "host/old-toolchain")
	if !ok || c.Status != Warn || !strings.Contains(c.Detail, "3 MB") || !strings.Contains(c.Detail, "delete") {
		t.Fatalf("old toolchain = %+v", c)
	}
}
