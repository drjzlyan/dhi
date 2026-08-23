package golden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripRemovesANSI(t *testing.T) {
	in := "\x1b[38;2;34;211;238mDHI\x1b[0m \x1b[1mbold\x1b[0m"
	want := "DHI bold"
	if got := Strip(in); got != want {
		t.Fatalf("Strip() = %q, want %q", got, want)
	}
}

func TestCompareDetectsMismatch(t *testing.T) {
	if err := Compare("x", "same", "same"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := Compare("x", "a", "b"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestSnapshotWriteAndVerifyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Write mode.
	os.Setenv(updateEnv, "1")
	defer os.Unsetenv(updateEnv)
	Snapshot(t, "roundtrip", "\x1b[1mstyled\x1b[0m\nlines")

	// Verify mode.
	os.Setenv(updateEnv, "")
	Snapshot(t, "roundtrip", "\x1b[31mstyled\x1b[0m\nlines") // ANSI ignored by design

	path := filepath.Join(dir, "testdata", "goldens", "roundtrip.golden")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden not written: %v", err)
	}
	if !strings.Contains(string(data), "styled\nlines") {
		t.Fatalf("golden contents unexpected: %q", string(data))
	}
}
