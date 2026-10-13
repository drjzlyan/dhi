package toolchain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOptionalToolchainsLive installs each optional toolchain from the
// real pins and runs a build or test with it (F-076; network, ~1.5 GB).
// Skipped unless DHI_SMOKE_OPTIONAL=1; DHI_SMOKE_ROOT reuses a prefix
// between runs. DHI_SMOKE_TOOLS narrows the set ("rust,jdk").
//
//	DHI_SMOKE_OPTIONAL=1 go test ./internal/toolchain/ -run TestOptionalToolchainsLive -timeout 30m
func TestOptionalToolchainsLive(t *testing.T) {
	if os.Getenv("DHI_SMOKE_OPTIONAL") != "1" {
		t.Skip("set DHI_SMOKE_OPTIONAL=1 to install and exercise the optional toolchains")
	}
	root := os.Getenv("DHI_SMOKE_ROOT")
	if root == "" {
		root = t.TempDir()
	}
	m := New(root)
	want := []string{"rust", "jdk", "maven", "gradle", "dotnet"}
	if s := os.Getenv("DHI_SMOKE_TOOLS"); s != "" {
		want = strings.Split(s, ",")
	}
	if err := m.InstallEmbedded(context.Background(), want); err != nil {
		t.Fatalf("install %v: %v", want, err)
	}
	env := append(append(m.Env(nil), m.BuildEnv()...), m.ToolEnv()...)
	run := func(dir string, argv ...string) string {
		t.Helper()
		cmd := exec.Command(filepath.Join(m.ShimDir(), argv[0]), argv[1:]...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", argv, err, out)
		}
		return string(out)
	}
	for _, sub := range WritableSubdirs {
		_ = os.MkdirAll(filepath.Join(root, sub), 0o755)
	}
	for _, tool := range want {
		dir := t.TempDir()
		switch tool {
		case "rust":
			write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
			write(t, dir, "src/lib.rs", "pub fn add(a: i32, b: i32) -> i32 { a + b }\n#[cfg(test)]\nmod t { #[test] fn a() { assert_eq!(super::add(1, 2), 3); } }\n")
			if out := run(dir, "cargo", "test", "--offline"); !strings.Contains(out, "test result: ok") {
				t.Fatalf("cargo test:\n%s", out)
			}
		case "jdk":
			write(t, dir, "A.java", "public class A { public static void main(String[] a) { System.out.println(\"ok-java\"); } }\n")
			run(dir, "javac", "A.java")
			if out := run(dir, "java", "A"); !strings.Contains(out, "ok-java") {
				t.Fatalf("java: %s", out)
			}
		case "maven":
			if out := run(dir, "mvn", "-v"); !strings.Contains(out, "Apache Maven") || !strings.Contains(out, filepath.Join(root, "tools", "jdk")) {
				t.Fatalf("mvn must run on the managed JDK:\n%s", out)
			}
		case "gradle":
			if out := run(dir, "gradle", "--version", "--offline"); !strings.Contains(out, "Gradle") {
				t.Fatalf("gradle: %s", out)
			}
		case "dotnet":
			if out := run(dir, "dotnet", "--version"); strings.TrimSpace(out) == "" {
				t.Fatalf("dotnet --version empty")
			}
		}
		t.Logf("%s ok", tool)
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
