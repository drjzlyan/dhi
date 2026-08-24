package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hermetic git layout (ADR-0009). The managed global config and an
// empty hooks dir live under <root>/git/, isolating agent-driven git
// operations from system config, user aliases, and hooks.
const (
	gitShimName = "git"
	gitDir      = "git"
	gitCfgRel   = gitDir + "/config"
	gitHooksRel = gitDir + "/hooks.d"
)

// GitBin is the shim-dir path of the hermetic git CLI. Presence is not
// implied — callers probe it (doctor, gitcore.ResolveRunner).
func (m *Manager) GitBin() string { return filepath.Join(m.ShimDir(), gitShimName) }

// managedGitVars are enforced for every DHI-driven git child process:
// no system/global config leakage, no interactive prompts hanging a
// turn. User terminals are unaffected — they use Env, not GitEnv.
var managedGitVars = []string{
	"GIT_CONFIG_NOSYSTEM",
	"GIT_TERMINAL_PROMPT",
	"GIT_CONFIG_GLOBAL",
}

// GitEnv returns the environment for DHI-driven git child processes:
// Env(base) plus the ADR-0009 hardening variables. Previously managed
// keys in base are replaced, never duplicated, so nesting GitEnv calls
// is idempotent.
func (m *Manager) GitEnv(base []string) []string {
	core := m.Env(base)
	managed := map[string]bool{}
	for _, k := range managedGitVars {
		managed[k] = true
	}
	out := make([]string, 0, len(core)+len(managedGitVars))
	for _, kv := range core {
		if i := strings.IndexByte(kv, '='); i > 0 && managed[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	global := filepath.Join(m.root, gitCfgRel)
	return append(out,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL="+global,
	)
}

// EnsureGitConfig materializes the managed global git config targeted
// by GitEnv: deterministic default branch and an empty hooks dir, so
// neither user hooks nor defaults leak into agent operations. Missing
// parents are created; an existing config is left untouched.
func (m *Manager) EnsureGitConfig() error {
	hooks := filepath.Join(m.root, gitHooksRel)
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return fmt.Errorf("toolchain: git hooks dir: %w", err)
	}
	cfgPath := filepath.Join(m.root, gitCfgRel)
	if _, err := os.Stat(cfgPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("toolchain: git config: %w", err)
	}
	cfg := "[init]\n\tdefaultBranch = main\n[core]\n\thooksPath = " +
		filepath.Join(hooks, "") + "\n"
	tmp, err := os.CreateTemp(filepath.Join(m.root, gitDir), ".config-*")
	if err != nil {
		return fmt.Errorf("toolchain: git config: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(cfg); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("toolchain: git config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("toolchain: git config: %w", err)
	}
	if err := os.Rename(name, cfgPath); err != nil {
		os.Remove(name)
		return fmt.Errorf("toolchain: git config: %w", err)
	}
	return nil
}
