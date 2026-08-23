package toolchain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// localFixtureModule creates a tiny module whose build output is a real
// executable — exercising the full build/activate/lock/shim pipeline
// without network.
func localFixtureModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := "module example.com/hello\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	main := "package main\n\nfunc main() { println(\"fixture-hello\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "hello", "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuildInstallLocalModule(t *testing.T) {
	root := t.TempDir()
	m := New(root)

	// seed the go shim as a real symlink to the host toolchain —
	// identical shape to production (linkShims creates symlinks).
	shimDir := m.ShimDir()
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hostGo, err := exec.LookPath("go")
	if err != nil {
		t.Skip("host go required for local-module build test")
	}
	goShim := filepath.Join(shimDir, "go")
	if err := os.Symlink(hostGo, goShim); err != nil {
		t.Fatal(err)
	}

	spec := BuildSpec{
		Name:     "hello",
		Version:  "v0.1.0",
		LocalDir: filepath.Join(localFixtureModule(t), "hello"),
		BinName:  "hello",
		Shims:    []string{"hello"},
	}
	if err := m.BuildInstall(context.Background(), spec); err != nil {
		t.Fatalf("BuildInstall: %v", err)
	}

	lf, err := m.ReadLockfile()
	if err != nil {
		t.Fatal(err)
	}
	locked, ok := lf.Tools["hello"]
	if !ok || locked.Version != "0.1.0" || len(locked.SHA256) != 64 {
		t.Fatalf("locked = %+v", locked)
	}

	shimTarget, err := os.Readlink(filepath.Join(shimDir, "hello"))
	if err != nil {
		t.Fatalf("shim missing: %v", err)
	}
	out, err := execCmd(shimTarget)
	if err != nil || !strings.Contains(out, "fixture-hello") {
		t.Errorf("built binary output = %q err = %v", out, err)
	}
}

func TestBuildInstallRequiresGoTool(t *testing.T) {
	m := New(t.TempDir())
	err := m.BuildInstall(context.Background(), Gopls())
	if err == nil || !strings.Contains(err.Error(), "requires the go tool") {
		t.Fatalf("err = %v", err)
	}
}

func execCmd(path string) (string, error) {
	out, err := osRun(path)
	return string(out), err
}

// TestLiveGoplsBuild exercises the real production path: pinned Go
// toolchain → gopls built from source → shim linked → binary runs.
// Heavier than the net smoke (minutes); gated separately:
//
//	DHI_SMOKE_BUILD=1 go test ./internal/toolchain/ -run TestLiveGoplsBuild -timeout 15m
func TestLiveGoplsBuild(t *testing.T) {
	if os.Getenv("DHI_SMOKE_BUILD") != "1" {
		t.Skip("set DHI_SMOKE_BUILD=1 to build real gopls through the pinned toolchain")
	}
	root := t.TempDir()
	m := New(root)
	if err := m.InstallEmbedded(context.Background(), []string{"go"}); err != nil {
		t.Fatalf("install go: %v", err)
	}
	if err := m.BuildInstall(context.Background(), Gopls()); err != nil {
		t.Fatalf("build gopls: %v", err)
	}

	shim := filepath.Join(m.ShimDir(), "gopls")
	target, err := os.Readlink(shim)
	if err != nil {
		t.Fatalf("gopls shim missing: %v", err)
	}
	out, err := exec.Command(target, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "gopls") {
		t.Errorf("gopls version = %q err=%v", out, err)
	}
}
