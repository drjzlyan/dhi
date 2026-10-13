package verifyplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectEcosystems(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		eco     string
		test    string
		prepare string
		tools   string
	}{
		{"go", map[string]string{"go.mod": "module x"}, Go, "go test ./...", "", "go"},
		{"npm", map[string]string{"package.json": `{"scripts":{"test":"vitest run"},"devDependencies":{"vitest":"1"}}`, "package-lock.json": "{}"},
			Node, "npm test", "npm ci --no-audit --no-fund", "node"},
		{"pnpm", map[string]string{"package.json": `{"scripts":{"test":"jest"},"dependencies":{"a":"1"}}`, "pnpm-lock.yaml": ""},
			Node, "corepack pnpm test", "corepack pnpm install --frozen-lockfile", "node"},
		{"yarn", map[string]string{"package.json": `{"scripts":{"test":"jest"},"dependencies":{"a":"1"}}`, "yarn.lock": ""},
			Node, "corepack yarn test", "corepack yarn install", "node"},
		{"node no deps", map[string]string{"package.json": `{"scripts":{"test":"node --test"}}`}, Node, "npm test", "", "node"},
		{"python pytest", map[string]string{"pyproject.toml": "[project]\nname='x'\n[dependency-groups]\ndev=['pytest']"},
			Python, "uv run --offline --no-sync pytest -q", "uv sync", "uv"},
		{"python unittest", map[string]string{"pyproject.toml": "[project]\nname='x'"},
			Python, "uv run --offline --no-sync python -m unittest discover", "uv sync", "uv"},
		{"python requirements", map[string]string{"requirements.txt": "pytest==8"},
			Python, "uv run --offline --no-sync pytest -q", "uv venv --allow-existing", "uv"},
		{"rust", map[string]string{"Cargo.toml": "[package]"}, Rust, "cargo test --offline", "cargo fetch", "rust"},
		{"maven", map[string]string{"pom.xml": "<project/>"}, Maven, "mvn -o -q -B test", "mvn -q -B dependency:go-offline", "jdk,maven"},
		{"gradle", map[string]string{"build.gradle.kts": ""}, Gradle, "gradle --offline --quiet test", "gradle --quiet dependencies", "jdk,gradle"},
		{"dotnet", map[string]string{"App.sln": "", "App.csproj": ""}, DotNet, "dotnet test App.sln --no-restore", "dotnet restore App.sln", "dotnet"},
	}
	for _, c := range cases {
		got := Detect(tree(t, c.files))
		if len(got) != 1 {
			t.Errorf("%s: %d targets", c.name, len(got))
			continue
		}
		g := got[0]
		prep := ""
		if len(g.Prepare) > 0 {
			prep = strings.Join(g.Prepare[0], " ")
		}
		if g.Ecosystem != c.eco || g.String() != c.test || prep != c.prepare || strings.Join(g.Tools, ",") != c.tools {
			t.Errorf("%s: got %s %q prepare %q tools %v", c.name, g.Ecosystem, g.String(), prep, g.Tools)
		}
	}
}

func TestDetectSkipsNonProjects(t *testing.T) {
	for _, files := range []map[string]string{
		{"README.md": "hi"},
		{"package.json": `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`},
		{"package.json": `{"name":"x"}`},
	} {
		if got := Detect(tree(t, files)); len(got) != 0 {
			t.Errorf("%v: detected %v", files, got)
		}
	}
	poly := Detect(tree(t, map[string]string{"go.mod": "module x", "Cargo.toml": ""}))
	if len(poly) != 2 || poly[0].Ecosystem != Go || poly[1].Ecosystem != Rust {
		t.Fatalf("polyglot = %v", poly)
	}
}

func TestPlanOverride(t *testing.T) {
	dir := tree(t, map[string]string{"go.mod": "module x"})
	if got := Plan(dir, Override{Argv: []string{"make", "check"}, Set: true}); len(got) != 1 || got[0].String() != "make check" || got[0].Ecosystem != Custom {
		t.Fatalf("override = %v", got)
	}
	if got := Plan(dir, Override{Set: true}); got != nil {
		t.Fatalf("empty override must disable verify: %v", got)
	}
	if got := Plan(dir, Override{}); len(got) != 1 || got[0].Ecosystem != Go {
		t.Fatalf("unset override = %v", got)
	}
}

func TestFingerprintTracksLockfiles(t *testing.T) {
	dir := tree(t, map[string]string{"package.json": `{"scripts":{"test":"jest"},"dependencies":{"a":"1"}}`, "package-lock.json": "{}"})
	tg := Detect(dir)[0]
	a := tg.Fingerprint()
	if b := tg.Fingerprint(); a != b {
		t.Fatal("fingerprint unstable")
	}
	_ = os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"v":2}`), 0o644)
	if c := tg.Fingerprint(); c == a {
		t.Fatal("lockfile change not seen")
	}
}

func TestEnvFailure(t *testing.T) {
	for out, want := range map[string]bool{
		"npm ERR! code ENOTFOUND registry.npmjs.org":                             true,
		"error: failed to spawn: `pytest`":                                       true,
		"error: attempting to make an HTTP request, but --offline was specified": true,
		"[ERROR] Failed to execute goal ... Could not resolve dependencies":      true,
		"error NU1301: Unable to load the service index":                         true,
		"--- FAIL: TestSplit (0.00s)":                                            false,
		"AssertionError: expected 3 to equal 4":                                  false,
		"test result: FAILED. 1 passed; 1 failed":                                false,
	} {
		if got := EnvFailure(out); got != want {
			t.Errorf("EnvFailure(%q) = %v", out, got)
		}
	}
}
