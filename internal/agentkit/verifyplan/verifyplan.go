// Package verifyplan decides how DHI checks a task worktree (F-076): it
// detects the project's ecosystem from its files and returns the command
// that installs dependencies (run with network, once per lockfile state)
// and the test command (run offline). Detection is pure — files in, plan
// out — and covers Go, JavaScript/TypeScript, Python, Rust, Java/Kotlin
// (Maven, Gradle) and .NET. A member's `verify` in workspace.toml
// overrides it.
package verifyplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Ecosystems.
const (
	Go     = "go"
	Node   = "node"
	Python = "python"
	Rust   = "rust"
	Maven  = "maven"
	Gradle = "gradle"
	DotNet = "dotnet"
	Custom = "custom"
)

// Target is one check in a directory.
type Target struct {
	Ecosystem string
	Dir       string
	// Prepare installs dependencies so Test can run offline (network
	// allowed, ask-gated). Empty = nothing to install.
	Prepare [][]string
	// Lockfiles fingerprint Prepare: it reruns only when they change.
	Lockfiles []string
	Test      []string
	// Tools are the managed toolchains the commands need ("go", "node",
	// "uv", "rust", "jdk", "maven", "gradle", "dotnet").
	Tools []string
}

// String is the test command as typed.
func (t Target) String() string { return strings.Join(t.Test, " ") }

// Override is a member's configured verify command: set with an empty
// argv turns verification off.
type Override struct {
	Argv []string
	Set  bool
}

// Plan returns the targets for dir: the override when set, else what
// Detect finds.
func Plan(dir string, o Override) []Target {
	if o.Set {
		if len(o.Argv) == 0 {
			return nil
		}
		return []Target{{Ecosystem: Custom, Dir: dir, Test: append([]string(nil), o.Argv...)}}
	}
	return Detect(dir)
}

// Detect inspects dir's top-level files. A polyglot repo yields one
// target per ecosystem found, in a stable order.
func Detect(dir string) []Target {
	has := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	var out []Target
	if has("go.mod") {
		// No prepare: go test reads the shared module cache, which the
		// agent's own builds already filled; a cold cache is named as an
		// environment problem rather than asking for network every time.
		out = append(out, Target{Ecosystem: Go, Dir: dir, Test: []string{"go", "test", "./..."}, Tools: []string{"go"}})
	}
	if t, ok := node(dir, has); ok {
		out = append(out, t)
	}
	if t, ok := python(dir, has); ok {
		out = append(out, t)
	}
	if has("Cargo.toml") {
		out = append(out, Target{Ecosystem: Rust, Dir: dir,
			Prepare: [][]string{{"cargo", "fetch"}}, Lockfiles: []string{"Cargo.toml", "Cargo.lock"},
			Test: []string{"cargo", "test", "--offline"}, Tools: []string{"rust"}})
	}
	switch {
	case has("pom.xml"):
		out = append(out, Target{Ecosystem: Maven, Dir: dir,
			Prepare: [][]string{{"mvn", "-q", "-B", "dependency:go-offline"}}, Lockfiles: []string{"pom.xml"},
			Test: []string{"mvn", "-o", "-q", "-B", "test"}, Tools: []string{"jdk", "maven"}})
	case has("build.gradle") || has("build.gradle.kts") || has("settings.gradle") || has("settings.gradle.kts"):
		out = append(out, Target{Ecosystem: Gradle, Dir: dir,
			Prepare:   [][]string{{"gradle", "--quiet", "dependencies"}},
			Lockfiles: []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.lockfile"},
			Test:      []string{"gradle", "--offline", "--quiet", "test"}, Tools: []string{"jdk", "gradle"}})
	}
	if proj := dotnetProject(dir); proj != "" {
		out = append(out, Target{Ecosystem: DotNet, Dir: dir,
			Prepare: [][]string{{"dotnet", "restore", proj}}, Lockfiles: []string{proj, "packages.lock.json"},
			Test: []string{"dotnet", "test", proj, "--no-restore"}, Tools: []string{"dotnet"}})
	}
	return out
}

// npmPlaceholder is the test script `npm init` writes: not a test suite.
var npmPlaceholder = regexp.MustCompile(`no test specified`)

func node(dir string, has func(string) bool) (Target, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return Target{}, false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return Target{}, false
	}
	test := strings.TrimSpace(pkg.Scripts["test"])
	if test == "" || npmPlaceholder.MatchString(test) {
		return Target{}, false
	}
	t := Target{Ecosystem: Node, Dir: dir, Tools: []string{"node"}}
	switch {
	case has("pnpm-lock.yaml"):
		t.Prepare = [][]string{{"corepack", "pnpm", "install", "--frozen-lockfile"}}
		t.Lockfiles = []string{"package.json", "pnpm-lock.yaml"}
		t.Test = []string{"corepack", "pnpm", "test"}
	case has("yarn.lock"):
		t.Prepare = [][]string{{"corepack", "yarn", "install"}}
		t.Lockfiles = []string{"package.json", "yarn.lock"}
		t.Test = []string{"corepack", "yarn", "test"}
	case has("package-lock.json"):
		t.Prepare = [][]string{{"npm", "ci", "--no-audit", "--no-fund"}}
		t.Lockfiles = []string{"package.json", "package-lock.json"}
		t.Test = []string{"npm", "test"}
	default:
		t.Prepare = [][]string{{"npm", "install", "--no-audit", "--no-fund"}}
		t.Lockfiles = []string{"package.json"}
		t.Test = []string{"npm", "test"}
	}
	if !needsInstall(raw) {
		t.Prepare = nil // no dependencies: nothing to fetch
	}
	return t, true
}

// needsInstall reports whether package.json declares any dependencies.
func needsInstall(raw []byte) bool {
	var pkg map[string]json.RawMessage
	if json.Unmarshal(raw, &pkg) != nil {
		return true
	}
	for _, k := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		if v, ok := pkg[k]; ok && len(strings.TrimSpace(string(v))) > 2 {
			return true
		}
	}
	return false
}

func python(dir string, has func(string) bool) (Target, bool) {
	reqs, _ := filepath.Glob(filepath.Join(dir, "requirements*.txt"))
	project := has("pyproject.toml")
	if !project && len(reqs) == 0 && !has("setup.py") && !has("setup.cfg") && !has("pytest.ini") && !has("tox.ini") {
		return Target{}, false
	}
	t := Target{Ecosystem: Python, Dir: dir, Tools: []string{"uv"}}
	switch {
	case project:
		t.Prepare = [][]string{{"uv", "sync"}}
		t.Lockfiles = []string{"pyproject.toml", "uv.lock"}
	case len(reqs) > 0:
		t.Prepare = [][]string{{"uv", "venv", "--allow-existing"}}
		for _, r := range reqs {
			name := filepath.Base(r)
			t.Prepare = append(t.Prepare, []string{"uv", "pip", "install", "-r", name})
			t.Lockfiles = append(t.Lockfiles, name)
		}
	}
	if usesPytest(dir, has, reqs) {
		t.Test = []string{"uv", "run", "--offline", "--no-sync", "pytest", "-q"}
	} else {
		t.Test = []string{"uv", "run", "--offline", "--no-sync", "python", "-m", "unittest", "discover"}
	}
	return t, true
}

// usesPytest: pytest only when the project declares it — a bare
// `pytest` on a project without it would fail as a missing tool.
func usesPytest(dir string, has func(string) bool, reqs []string) bool {
	if has("pytest.ini") || has("conftest.py") {
		return true
	}
	for _, f := range append([]string{filepath.Join(dir, "pyproject.toml"), filepath.Join(dir, "setup.cfg"), filepath.Join(dir, "tox.ini")}, reqs...) {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(strings.ToLower(string(b)), "pytest") {
			return true
		}
	}
	return false
}

// dotnetProject returns the solution (preferred) or the single project
// file in dir; "" when there is none or the choice is ambiguous.
func dotnetProject(dir string) string {
	for _, pat := range []string{"*.sln", "*.slnx", "*.csproj", "*.fsproj", "*.vbproj"} {
		m, _ := filepath.Glob(filepath.Join(dir, pat))
		sort.Strings(m)
		if len(m) == 1 {
			return filepath.Base(m[0])
		}
		if len(m) > 1 && strings.HasPrefix(pat, "*.sln") {
			return filepath.Base(m[0])
		}
	}
	return ""
}

// Fingerprint hashes the target's lockfiles (missing ones count as
// absent), so Prepare reruns exactly when dependencies may have changed.
func (t Target) Fingerprint() string {
	h := sha256.New()
	for _, f := range t.Lockfiles {
		h.Write([]byte(f + "\x00"))
		if b, err := os.ReadFile(filepath.Join(t.Dir, f)); err == nil {
			h.Write(b)
		}
		h.Write([]byte{0})
	}
	for _, p := range t.Prepare {
		h.Write([]byte(strings.Join(p, " ") + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// envSignatures mark a verify that could not run here — no network, a
// missing toolchain, a sandbox denial — as opposed to failing tests.
var envSignatures = []string{
	// shared
	"dial tcp", "connection refused", "no such host", "operation not permitted",
	"network is unreachable", "could not resolve host", "executable file not found",
	// go
	"proxy.golang.org", "cannot find module providing", "no required module provides", "go: downloading",
	// node
	"enotfound", "eai_again", "err_pnpm_", "npm err! code e404",
	// python / uv
	"failed to spawn", "no virtual environment found", "modulenotfounderror: no module named 'pytest'",
	"network connectivity is disabled",
	// rust
	"failed to get `", "attempting to make an http request, but --offline was specified", "no matching package named",
	// jvm
	"could not resolve dependencies", "cannot access central", "could not resolve all files",
	"java_home is not defined", "no compiler is provided",
	// dotnet
	"nu1301", "nu1101", "unable to load the service index", "was not found. run a nuget package restore",
}

// EnvFailure reports whether output names an environment problem.
func EnvFailure(output string) bool {
	low := strings.ToLower(output)
	for _, s := range envSignatures {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}
