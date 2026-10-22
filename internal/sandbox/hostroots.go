package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// Host roots (F-113, ADR-0044): agents use the user's own tools, so the
// folders those tools run from are readable and executable, and the
// standard per-user package caches are writable — a build or a dependency
// install works inside the boundary without a card. Everything else
// outside the workspace still asks.

// PackageCaches lists the existing per-user package caches under home:
// npm, pnpm, bun, Go's module and build caches, cargo's registry, Gradle,
// Maven, pip/uv (~/.cache on Linux), and ~/Library/Caches on macOS.
// getenv lets a cache moved by GOMODCACHE/GOCACHE/npm_config_cache count.
func PackageCaches(home, goos string, getenv func(string) string) []string {
	if home == "" {
		return nil
	}
	cand := []string{
		filepath.Join(home, ".npm"),
		filepath.Join(home, ".cache"),
		filepath.Join(home, "go", "pkg", "mod"),
		// cargo locks files in CARGO_HOME itself and Gradle keeps its
		// daemon beside the caches, so their homes are writable; the
		// credential files in them stay fenced (CredentialPaths).
		filepath.Join(home, ".cargo"),
		filepath.Join(home, ".gradle"),
		filepath.Join(home, ".m2", "repository"),
		filepath.Join(home, ".bun", "install", "cache"),
		filepath.Join(home, ".local", "share", "pnpm", "store"),
	}
	if goos == "darwin" {
		cand = append(cand, filepath.Join(home, "Library", "Caches"))
	}
	if getenv != nil {
		for _, k := range []string{"GOMODCACHE", "GOCACHE", "npm_config_cache", "UV_CACHE_DIR", "PIP_CACHE_DIR", "CARGO_HOME", "GRADLE_USER_HOME"} {
			if v := getenv(k); filepath.IsAbs(v) {
				cand = append(cand, v)
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range cand {
		r, err := filepath.EvalSymlinks(c)
		if err != nil || seen[r] {
			continue // only existing caches: bubblewrap binds what exists
		}
		if fi, err := os.Stat(r); err != nil || !fi.IsDir() {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// ToolHomes are toolchain homes a PATH entry runs from without being on
// PATH itself: rustup's proxies in ~/.cargo/bin exec the real cargo under
// ~/.rustup. Read and execute only.
func ToolHomes(home string, getenv func(string) string) []string {
	cand := []string{filepath.Join(home, ".rustup")}
	if getenv != nil {
		if v := getenv("RUSTUP_HOME"); filepath.IsAbs(v) {
			cand = append(cand, v)
		}
	}
	var out []string
	for _, c := range cand {
		if r, err := filepath.EvalSymlinks(c); err == nil {
			out = append(out, r)
		}
	}
	return out
}

// ExecRoots turns the user's PATH into read+exec roots: each existing
// directory, widened to its prefix when it is a "bin" folder (Homebrew's
// /opt/homebrew, nvm's versions/node/vX, ~/.local) so a tool's own
// libraries resolve. Never the home directory or / itself.
func ExecRoots(path, home string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(path) {
		if !filepath.IsAbs(d) {
			continue
		}
		r, err := filepath.EvalSymlinks(d)
		if err != nil {
			continue
		}
		if filepath.Base(r) == "bin" {
			if p := filepath.Dir(r); p != "/" && p != home && !strings.HasPrefix(home, p+string(filepath.Separator)) {
				r = p
			}
		}
		if r == "/" || r == home || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}
