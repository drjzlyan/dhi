package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeNPM installs a shim `npm` that records how it was called and
// fabricates node_modules/.bin/<name> like a real install would.
func fakeNPM(t *testing.T, body string) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m := New(root)
	if err := os.MkdirAll(m.ShimDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "npm.log")
	script := "#!/bin/sh\n" +
		"echo \"args: $*\" >> " + log + "\n" +
		"echo \"cache: $npm_config_cache\" >> " + log + "\n" +
		"echo \"path0: ${PATH%%:*}\" >> " + log + "\n" +
		body
	if err := os.WriteFile(filepath.Join(m.ShimDir(), "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return m, log
}

func TestNPMInstallRunsTheHermeticNpmWithPrivateCacheAndPrefix(t *testing.T) {
	m, log := fakeNPM(t, `prefix=""
while [ $# -gt 0 ]; do [ "$1" = "--prefix" ] && prefix="$2"; shift; done
mkdir -p "$prefix/node_modules/.bin" && printf '#!/bin/sh\necho 1.0.0\n' > "$prefix/node_modules/.bin/codex" && chmod +x "$prefix/node_modules/.bin/codex"
`)
	prefix := filepath.Join(m.Root(), "clis", "codex")
	bin, err := m.NPMInstall(context.Background(), prefix, "@openai/codex")
	if err != nil {
		t.Fatal(err)
	}
	if bin != filepath.Join(prefix, "node_modules", ".bin") {
		t.Fatalf("bin dir = %q", bin)
	}
	if _, err := os.Stat(filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(log)
	for _, want := range []string{
		"args: install --prefix " + prefix + " --no-audit --no-fund --no-update-notifier --loglevel=error @openai/codex",
		"cache: " + filepath.Join(m.Root(), "npm-cache"),
		"path0: " + m.ShimDir(),
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("npm call lacks %q:\n%s", want, got)
		}
	}
}

func TestNPMInstallRefusesWithoutHermeticNPM(t *testing.T) {
	m := New(t.TempDir())
	_, err := m.NPMInstall(context.Background(), filepath.Join(m.Root(), "clis", "x"), "pkg")
	if err == nil || !strings.Contains(err.Error(), "not installed in the DHI toolchain") {
		t.Fatalf("err = %v", err)
	}
}

func TestNPMInstallFailureCarriesTheTail(t *testing.T) {
	m, _ := fakeNPM(t, "echo 'npm ERR! 404 Not Found - GET registry/@nope/pkg' >&2\nexit 1\n")
	_, err := m.NPMInstall(context.Background(), filepath.Join(m.Root(), "clis", "x"), "@nope/pkg")
	if err == nil || !strings.Contains(err.Error(), "404 Not Found") {
		t.Fatalf("err = %v", err)
	}
}

func TestNPMInstallDetectsAnInstallThatProducedNothing(t *testing.T) {
	m, _ := fakeNPM(t, "exit 0\n")
	_, err := m.NPMInstall(context.Background(), filepath.Join(m.Root(), "clis", "x"), "pkg")
	if err == nil || !strings.Contains(err.Error(), "no executables") {
		t.Fatalf("err = %v", err)
	}
}

func TestNPMInstallValidatesArguments(t *testing.T) {
	m, _ := fakeNPM(t, "exit 0\n")
	if _, err := m.NPMInstall(context.Background(), "", "pkg"); err == nil {
		t.Fatal("empty prefix accepted")
	}
	if _, err := m.NPMInstall(context.Background(), filepath.Join(m.Root(), "x"), ""); err == nil {
		t.Fatal("empty package accepted")
	}
}

func TestNPMInstallPassesEveryPackageToOneRun(t *testing.T) {
	m, log := fakeNPM(t, `prefix=""
while [ $# -gt 0 ]; do [ "$1" = "--prefix" ] && prefix="$2"; shift; done
mkdir -p "$prefix/node_modules/.bin"
`)
	if _, err := m.NPMInstall(context.Background(), filepath.Join(m.Root(), "lsp", "typescript"),
		"typescript-language-server@6.0.1", "typescript@5.9.3"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(log)
	if !strings.Contains(string(got), " typescript-language-server@6.0.1 typescript@5.9.3\n") {
		t.Fatalf("both packages must reach one npm run:\n%s", got)
	}
}
