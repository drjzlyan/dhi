package toolchain

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Env returns the environment for DHI's child processes (terminals, LSP
// servers, git, agents): the shim directory is prepended to PATH so the
// hermetic tools resolve first — and only here. The user's shell and
// desktop are never touched (ADR-0005).
//
// A nil base inherits os.Environ(); an explicitly empty base stays
// empty apart from the shim-only PATH, keeping fully isolated children
// free of host leakage.
func (m *Manager) Env(base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	shim := m.ShimDir()
	out := make([]string, 0, len(base)+1)
	sawPath := false
	for _, kv := range base {
		if kv == "PATH=" || strings.HasPrefix(kv, "PATH=") {
			cur := strings.TrimPrefix(kv, "PATH=")
			if firstPathEntry(cur) == shim {
				out = append(out, kv) // idempotent: already shim-first
			} else if cur == "" {
				out = append(out, "PATH="+shim)
			} else {
				out = append(out, "PATH="+shim+string(os.PathListSeparator)+cur)
			}
			sawPath = true
			continue
		}
		out = append(out, kv)
	}
	if !sawPath {
		switch {
		case len(base) == 0: // explicitly empty env stays host-free
			out = append(out, "PATH="+shim)
		case os.Getenv("PATH") == "":
			out = append(out, "PATH="+shim)
		default:
			out = append(out, "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	}
	return out
}

func firstPathEntry(pathList string) string {
	first, _, _ := strings.Cut(pathList, string(os.PathListSeparator))
	return first
}

// WritableSubdirs are the toolchain folders sandboxed agent processes may
// write: npm's cache (npx-run MCP servers) and the MCP servers' private
// HOME. Everything else under the prefix is read-only to them.
var WritableSubdirs = []string{"npm-cache", "mcp-home",
	// F-076: build caches for the ecosystems DHI verifies.
	"uv-cache", "cargo-home", "gradle-home", "m2", "nuget", "dotnet-home",
	// F-086 (live): go build/test write GOCACHE before anything else; the
	// default ~/Library/Caches/go-build is outside the sandbox.
	"go-cache", "go-path"}

// BuildEnv points the package managers' caches at the toolchain's
// writable folders (F-076): a sandboxed build may not write $HOME
// (~/.cargo, ~/.m2, ~/.gradle, ~/.nuget), and a denied cache write would
// read as an environment failure. It is for DHI's command runner and the
// agent CLIs — the user's own terminals keep their home caches.
func (m *Manager) BuildEnv() []string {
	sub := func(name string) string { return filepath.Join(m.root, name) }
	return []string{
		"npm_config_cache=" + sub("npm-cache"),
		"npm_config_update_notifier=false",
		"COREPACK_HOME=" + filepath.Join(sub("npm-cache"), "corepack"),
		"UV_CACHE_DIR=" + sub("uv-cache"),
		"UV_PYTHON_INSTALL_DIR=" + filepath.Join(sub("uv-cache"), "python"),
		"CARGO_HOME=" + sub("cargo-home"),
		"GRADLE_USER_HOME=" + sub("gradle-home"),
		"MAVEN_OPTS=-Dmaven.repo.local=" + filepath.Join(sub("m2"), "repository"),
		"NUGET_PACKAGES=" + sub("nuget"),
		"DOTNET_CLI_HOME=" + sub("dotnet-home"),
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"DOTNET_NOLOGO=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"GOCACHE=" + sub("go-cache"),
		"GOPATH=" + sub("go-path"),
		"GOMODCACHE=" + filepath.Join(sub("go-path"), "pkg", "mod"),
	}
}

// ToolEnv is the environment the managed tools declare (JAVA_HOME,
// DOTNET_ROOT — F-076), pointing at each tool's stable current/ link.
// It is computed from the embedded manifest at launch, so a tool
// installed mid-session is reachable without a restart.
func (m *Manager) ToolEnv() []string {
	mf, err := Embedded()
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range sortedTools(mf) {
		spec, ok := mf.Tools[name].Platforms[PlatformKey()]
		if !ok {
			continue
		}
		home := filepath.Join(m.root, "tools", name, "current")
		keys := make([]string, 0, len(spec.Env))
		for k := range spec.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, k+"="+strings.ReplaceAll(spec.Env[k], "{home}", home))
		}
	}
	return out
}

func sortedTools(mf *Manifest) []string {
	out := make([]string, 0, len(mf.Tools))
	for n := range mf.Tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Installed reports whether name is in the lockfile.
func (m *Manager) Installed(name string) bool {
	lf, err := m.ReadLockfile()
	if err != nil {
		return false
	}
	_, ok := lf.Tools[name]
	return ok
}
