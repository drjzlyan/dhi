package clirun

import (
	"os"
	"path/filepath"
)

// InstallMethod says how DHI gets a CLI onto the machine (ADR-0027).
type InstallMethod string

const (
	// MethodNPM: installed by DHI's own npm into a DHI-owned folder, after
	// the user confirms the exact command.
	MethodNPM InstallMethod = "npm"
	// MethodManual: DHI shows the vendor's command and docs and re-detects;
	// it never runs a remote script.
	MethodManual InstallMethod = "manual"
)

// InstallPlan describes the one supported way to install a CLI. Every
// field is a verified fact from the vendor's documentation (F-046) or
// empty — never a guess.
type InstallPlan struct {
	CLI     string
	Method  InstallMethod
	Package string // npm package (MethodNPM)
	Command string // vendor command shown for MethodManual ("" = none verified)
	Docs    string // vendor documentation URL ("" = none verified)
	Auth    string // how the user signs in once installed
	Note    string // caveats worth showing
}

var plans = map[string]InstallPlan{
	"claude": {
		CLI: "claude", Method: MethodNPM, Package: "@anthropic-ai/claude-code",
		Docs: "https://code.claude.com/docs/en/setup",
		Auth: "run `claude` and follow the browser prompts (or set ANTHROPIC_API_KEY)",
		Note: "Anthropic's recommended installer is the native script; the npm package installs the same binary.",
	},
	"codex": {
		CLI: "codex", Method: MethodNPM, Package: "@openai/codex",
		Docs: "https://github.com/openai/codex",
		Auth: "run `codex` and choose “Sign in with ChatGPT” (or use an API key)",
	},
	"opencode": {
		CLI: "opencode", Method: MethodNPM, Package: "opencode-ai",
		Docs: "https://opencode.ai/docs/",
		Auth: "run `opencode`, then `/connect` to add a provider",
	},
	"copilot": {
		CLI: "copilot", Method: MethodNPM, Package: "@github/copilot",
		Docs: "https://github.com/github/copilot-cli",
		Auth: "run `copilot`, then `/login` (or set GH_TOKEN)",
		Note: "Requires an active GitHub Copilot subscription.",
	},
	"cursor-agent": {
		CLI: "cursor-agent", Method: MethodManual,
		Command: "curl https://cursor.com/install -fsS | bash",
		Docs:    "https://cursor.com/docs/cli/overview",
		Auth:    "sign in from the CLI itself (see Cursor's docs)",
		Note:    "Cursor documents a shell script only, which DHI does not run. Its binary may be named `agent`; DHI looks for `cursor-agent`.",
	},
	"antigravity": {
		CLI: "antigravity", Method: MethodManual,
		Note: "DHI has no verified install command for Antigravity's `agy`; install it from Google's official page.",
	},
}

// PlanFor returns the install plan for a registered CLI.
func PlanFor(name string) (InstallPlan, bool) {
	p, ok := plans[name]
	return p, ok
}

// ManagedSubdir is where DHI-installed CLIs live under the toolchain root.
const ManagedSubdir = "clis"

// ManagedDir is the install prefix for one CLI.
func ManagedDir(toolRoot, name string) string {
	return filepath.Join(toolRoot, ManagedSubdir, name)
}

// ManagedBinDir is where npm puts a managed CLI's executables.
func ManagedBinDir(toolRoot, name string) string {
	return filepath.Join(ManagedDir(toolRoot, name), "node_modules", ".bin")
}

// ManagedLook wraps a PATH lookup: the user's own install always wins; the
// DHI-managed folders are consulted only when the binary is not on PATH.
func ManagedLook(toolRoot string, fallback func(string) (string, error)) func(string) (string, error) {
	return func(bin string) (string, error) {
		p, err := fallback(bin)
		if err == nil || toolRoot == "" {
			return p, err
		}
		entries, rerr := os.ReadDir(filepath.Join(toolRoot, ManagedSubdir))
		if rerr != nil {
			return p, err
		}
		for _, e := range entries {
			cand := filepath.Join(ManagedBinDir(toolRoot, e.Name()), bin)
			if fi, serr := os.Stat(cand); serr == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return cand, nil
			}
		}
		return p, err
	}
}
