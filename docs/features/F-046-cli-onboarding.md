# F-046: Coding-CLI onboarding

Status: complete (M25 P5) · ADR-0027 · Builds on F-013 (CLI runtimes), F-043 (wizard).

## Problem
A new user needs a working coding CLI before any employee can think. Today DHI
only reports a missing one in `doctor`, and any CLI update beyond the pinned
version turns doctor red.

## Design
- **Catalog** (`clirun.PlanFor`): per CLI, how it is installed — verified against
  vendor docs on 2026-10-08:

  | CLI | Route | Source |
  |---|---|---|
  | claude | npm `@anthropic-ai/claude-code` (Node 22+, installs the native binary) | code.claude.com/docs/en/setup |
  | codex | npm `@openai/codex` | github.com/openai/codex |
  | opencode | npm `opencode-ai` | opencode.ai/docs |
  | copilot | npm `@github/copilot` | github.com/github/copilot-cli |
  | cursor-agent | manual: `curl https://cursor.com/install -fsS \| bash` (binary may be named `agent`) | cursor.com/docs/cli/overview |
  | antigravity | manual; no verified command | — |

  Each plan also carries a one-line "how to sign in".
- **Managed install** (`toolchain.Manager.NPMInstall`): `npm install --prefix
  <root>/clis/<name> <pkg>` with the hermetic node, a private cache under the
  prefix, no audit/fund/update-notifier noise, 5-minute cap. Missing npm →
  named refusal pointing at the toolchain bootstrap.
- **Lookup** (`clirun.ManagedLook`): PATH first, then `<root>/clis/*/node_modules/.bin`.
  Used by the runtime registry, Settings detection and doctor, so an installed CLI
  is used everywhere.
- **Version policy** (`clirun.Assess`, ADR-0027 §6) drives doctor and the wizard.
- **Wizard step "CLI"** (before "team"): list of CLIs with version/status;
  `i` installs the highlighted npm-route CLI after showing the exact command;
  manual CLIs show the command + docs; `r` re-detects; the highlighted CLI shows
  its sign-in hint. A successful install marks the run changed (relaunch).

## Acceptance criteria
- [x] Assess: equal/same-major-drift/major-change/unparseable, table-tested
- [x] doctor: drift warns, major change fails, missing fails with an install pointer; managed installs are found
- [x] catalog: every registered CLI has a plan; npm plans name a package; manual plans never auto-run
- [x] NPMInstall: argv/env/cache exactly as specified (fake npm), missing npm refuses, failure carries the tail of the output
- [x] ManagedLook: PATH wins, then managed dir, then not found
- [x] wizard step: list, confirm shows exact command, install runs async with spinner, navigation blocked while it runs, result re-detects, failure keeps the step with the reason, manual shows command, relaunch flagged
- [x] walked end to end in tmux (restricted PATH, fake npm): detect → confirm → install → managed CLI detected → team engine defaults to it → relaunch; `doctor` finds it

## Not verified here
A real vendor package install (the tests and the tmux run use a fake `npm`, so no
vendor download happens). The install commands themselves are taken from vendor
documentation read on 2026-10-08; re-verify them when bumping a pin. Antigravity
has no verified command at all and stays guided-manual.

## Deferred
Authentication probing; native vendor installers; updating installed CLIs.
