package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageCachesAreTheExistingOnes(t *testing.T) {
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	os.MkdirAll(filepath.Join(home, ".npm"), 0o755)
	os.MkdirAll(filepath.Join(home, "go", "pkg", "mod"), 0o755)
	os.MkdirAll(filepath.Join(home, "Library", "Caches"), 0o755)
	moved := filepath.Join(home, "gocache")
	os.MkdirAll(moved, 0o755)
	got := PackageCaches(home, "darwin", func(k string) string {
		if k == "GOCACHE" {
			return moved
		}
		return ""
	})
	want := []string{filepath.Join(home, ".npm"), filepath.Join(home, "go", "pkg", "mod"), filepath.Join(home, "Library", "Caches"), moved}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("caches = %v\nwant %v", got, want)
	}
	if got := PackageCaches(home, "linux", nil); strings.Contains(strings.Join(got, ","), "Library") {
		t.Fatalf("linux has no ~/Library/Caches: %v", got)
	}
}

func TestExecRootsWidenBinFoldersButNeverHome(t *testing.T) {
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	brew := filepath.Join(home, "opt", "homebrew", "bin")
	nvm := filepath.Join(home, ".nvm", "versions", "node", "v24", "bin")
	homeBin := filepath.Join(home, "bin")
	for _, d := range []string{brew, nvm, homeBin} {
		os.MkdirAll(d, 0o755)
	}
	path := strings.Join([]string{brew, nvm, homeBin, "relative/bin", "/", filepath.Join(home, "missing")}, string(os.PathListSeparator))
	got := ExecRoots(path, home)
	want := []string{filepath.Dir(brew), filepath.Dir(nvm), homeBin}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roots = %v\nwant %v", got, want)
	}
}

// A missing bwrap does not fail Select: agents name the install at each
// spawn, and work once bwrap is installed (no restart).
func TestMissingBwrapIsNamedAtSpawnNotAtBoot(t *testing.T) {
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	sb, err := Select("linux", missing, "auto", []string{"/w"}, nil)
	if sb == nil || !errors.Is(err, ErrHelperMissing) {
		t.Fatalf("sb=%v err=%v", sb, err)
	}
	orig := systemLookPath
	t.Cleanup(func() { systemLookPath = orig })
	systemLookPath = missing
	if _, err := sb.Wrap([]string{"true"}); err == nil || !strings.Contains(err.Error(), "install bwrap") {
		t.Fatalf("spawn without bwrap = %v", err)
	}
	systemLookPath = func(string) (string, error) { return "/usr/bin/bwrap", nil }
	argv, err := sb.Wrap([]string{"true"})
	if err != nil || argv[0] != "/usr/bin/bwrap" {
		t.Fatalf("after install = %v %v", argv, err)
	}
}
