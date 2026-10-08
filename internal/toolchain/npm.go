package toolchain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// npmTimeout caps one package install.
const npmTimeout = 5 * time.Minute

// NPMInstall installs an npm package into prefix using DHI's own pinned
// node/npm (ADR-0027): `npm install --prefix <prefix> <pkg>` with a private
// cache under the toolchain root, so nothing lands in ~/.npm or any global
// directory and no sudo is involved. It returns the directory that holds the
// package's executables. A missing hermetic npm is a named refusal pointing at
// the bootstrap, never a fallback to a host npm (ADR-0011).
func (m *Manager) NPMInstall(ctx context.Context, prefix, pkg string) (binDir string, err error) {
	npm := filepath.Join(m.ShimDir(), "npm")
	if _, serr := os.Stat(npm); serr != nil {
		return "", fmt.Errorf("toolchain: npm is not installed in the DHI toolchain (finish the first-run install, then retry)")
	}
	if pkg == "" || prefix == "" {
		return "", fmt.Errorf("toolchain: npm install needs a package and a prefix")
	}
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return "", fmt.Errorf("toolchain: npm prefix: %w", err)
	}
	cache := filepath.Join(m.root, "npm-cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", fmt.Errorf("toolchain: npm cache: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, npmTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, npm,
		"install", "--prefix", prefix,
		"--no-audit", "--no-fund", "--no-update-notifier", "--loglevel=error",
		pkg)
	cmd.Dir = prefix
	cmd.Env = append(m.Env(nil),
		"npm_config_cache="+cache,
		"npm_config_update_notifier=false",
	)
	cmd.WaitDelay = 500 * time.Millisecond
	out, rerr := cmd.CombinedOutput()
	if rerr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("toolchain: npm install %s timed out after %s", pkg, npmTimeout)
		}
		return "", fmt.Errorf("toolchain: npm install %s: %v: %s", pkg, rerr, tailRunes(out, 400))
	}
	bin := filepath.Join(prefix, "node_modules", ".bin")
	if _, serr := os.Stat(bin); serr != nil {
		return "", fmt.Errorf("toolchain: npm install %s succeeded but produced no executables", pkg)
	}
	return bin, nil
}
