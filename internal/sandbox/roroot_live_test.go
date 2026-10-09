package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestReadOnlyRootWithWritableChildLive runs real commands under the
// platform sandbox (skipped where the helper is missing): inside a
// read-only root (the toolchain) a write is refused, a writable child
// (its npm cache) accepts writes, and reads work. This is the ro-root
// differentiation boot relies on to keep agents from replacing DHI's
// pinned binaries.
func TestReadOnlyRootWithWritableChildLive(t *testing.T) {
	var bin string
	switch runtime.GOOS {
	case "darwin":
		bin, _ = exec.LookPath("sandbox-exec")
	case "linux":
		bin, _ = exec.LookPath("bwrap")
	}
	if bin == "" {
		t.Skip("no OS sandbox helper on this machine")
	}
	if runtime.GOOS == "linux" {
		// Hosted runners may forbid unprivileged user namespaces.
		if err := exec.Command(bin, "--ro-bind", "/", "/", "true").Run(); err != nil {
			t.Skipf("bwrap cannot run here (user namespaces restricted?): %v", err)
		}
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws, tool := filepath.Join(base, "ws"), filepath.Join(base, "tool")
	cache := filepath.Join(tool, "npm-cache")
	for _, d := range []string{ws, cache} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tool, "go"), []byte("pinned"), 0o644); err != nil {
		t.Fatal(err)
	}
	sb, err := Select(runtime.GOOS, exec.LookPath, "auto", []string{ws, cache}, []string{tool})
	if err != nil {
		t.Fatal(err)
	}
	run := func(script string) error {
		argv, err := sb.Wrap([]string{"/bin/sh", "-c", script})
		if err != nil {
			t.Fatal(err)
		}
		return exec.Command(argv[0], argv[1:]...).Run()
	}
	if err := run("echo replaced > " + filepath.Join(tool, "go")); err == nil {
		t.Fatal("a sandboxed process overwrote a file in the read-only toolchain")
	}
	if data, _ := os.ReadFile(filepath.Join(tool, "go")); string(data) != "pinned" {
		t.Fatalf("toolchain file changed: %q", data)
	}
	if err := run("echo ok > " + filepath.Join(cache, "entry")); err != nil {
		t.Fatalf("the writable cache inside the read-only root refused a write: %v", err)
	}
	if err := run("cat " + filepath.Join(tool, "go")); err != nil {
		t.Fatalf("reading the read-only root failed: %v", err)
	}
	if err := run("echo ok > " + filepath.Join(ws, "f")); err != nil {
		t.Fatalf("workspace write refused: %v", err)
	}
}
