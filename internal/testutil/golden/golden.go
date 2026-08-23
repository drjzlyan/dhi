// Package golden provides the deterministic snapshot harness used across DHI.
//
// Golden files live in <package>/testdata/goldens/<name>.golden and store
// ANSI-stripped text so layout is asserted while colors stay free to evolve
// (color assertions are done separately with targeted unit tests).
//
// Regenerate after an intentional visual change:
//
//	DHI_UPDATE_GOLDENS=1 go test ./...
package golden

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const updateEnv = "DHI_UPDATE_GOLDENS"

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-B]`)

// Strip removes ANSI escape sequences, leaving plain text for readable diffs.
func Strip(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Compare is the pure comparison core behind Snapshot; exported for tests.
func Compare(name, want, got string) error {
	if want != got {
		return errors.New("golden mismatch for " + name +
			"\n--- want ---\n" + want + "\n--- got ---\n" + got +
			"\nIf intentional, regenerate: DHI_UPDATE_GOLDENS=1 go test ./...")
	}
	return nil
}

// Snapshot compares actual against the stored golden file. With
// DHI_UPDATE_GOLDENS set, it (re)writes the golden instead of comparing.
func Snapshot(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", "goldens", name+".golden")
	plain := strings.TrimRight(Strip(actual), "\n") + "\n"

	if os.Getenv(updateEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden: mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(plain), 0o644); err != nil {
			t.Fatalf("golden: write %s: %v", path, err)
		}
		return
	}

	expected, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("golden file missing: %s\nRun DHI_UPDATE_GOLDENS=1 go test ./... to create it.", path)
	}
	if err != nil {
		t.Fatalf("golden: read %s: %v", path, err)
	}
	if cerr := Compare(name, string(expected), plain); cerr != nil {
		t.Fatal(cerr)
	}
}

// Contains asserts that the ANSI-stripped actual contains substr, producing a
// diff-friendly failure otherwise. Useful for spot-checks where a full golden
// would be brittle.
func Contains(t *testing.T, name, actual, substr string) {
	t.Helper()
	if !strings.Contains(Strip(actual), substr) {
		t.Fatalf("%s: rendered output does not contain %q\ngot:\n%s", name, substr, Strip(actual))
	}
}
