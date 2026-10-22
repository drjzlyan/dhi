package clirun

// InstallPlan is what DHI knows about getting a CLI (ADR-0044): where the
// vendor documents it, how to sign in, and caveats. The install command
// itself comes from internal/hosttools (the user's package manager, run
// only on approval); Command is a vendor-documented command DHI shows but
// never runs (a remote script).
type InstallPlan struct {
	CLI     string
	Command string // vendor command shown when no package manager carries it ("" = none verified)
	Docs    string // vendor documentation URL ("" = none verified)
	Auth    string // how the user signs in once installed
	Note    string // caveats worth showing
}

var plans = map[string]InstallPlan{
	"claude": {
		CLI:  "claude",
		Docs: "https://code.claude.com/docs/en/setup",
		Auth: "run `claude` and follow the browser prompts (or set ANTHROPIC_API_KEY)",
	},
	"codex": {
		CLI:  "codex",
		Docs: "https://github.com/openai/codex",
		Auth: "run `codex` and choose “Sign in with ChatGPT” (or use an API key)",
	},
	"opencode": {
		CLI:  "opencode",
		Docs: "https://opencode.ai/docs/",
		Auth: "run `opencode`, then `/connect` to add a provider",
	},
	"copilot": {
		CLI:  "copilot",
		Docs: "https://github.com/github/copilot-cli",
		Auth: "run `copilot`, then `/login` (or set GH_TOKEN)",
		Note: "Requires an active GitHub Copilot subscription.",
	},
	"cursor-agent": {
		CLI:     "cursor-agent",
		Command: "curl https://cursor.com/install -fsS | bash",
		Docs:    "https://cursor.com/docs/cli/overview",
		Auth:    "sign in from the CLI itself (see Cursor's docs)",
		Note:    "Cursor documents a shell script only, which DHI does not run. Its binary may be named `agent`; DHI looks for `cursor-agent`.",
	},
	"antigravity": {
		CLI:  "antigravity",
		Note: "DHI has no verified install command for Antigravity's `agy`; install it from Google's official page.",
	},
}

// PlanFor returns the install plan for a registered CLI.
func PlanFor(name string) (InstallPlan, bool) {
	p, ok := plans[name]
	return p, ok
}
