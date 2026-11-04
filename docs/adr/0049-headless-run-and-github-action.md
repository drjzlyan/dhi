# ADR-0049: A non-interactive `dhi run` and a GitHub Action

Date: 2026-11-04 · Status: proposed · Companion to: ADR-0004 (minimal CLI), ADR-0011, ADR-0044,
ADR-0032 (goal loop envelope), ADR-0046
Serves: gap G-C6.

## Context
ADR-0004 limits the binary to bare launch, `doctor [--json]`, `version` and `help`
(`cmd/dhi/main.go`); everything else is an in-TUI flow so there is one interaction model and one
test harness. Its reasoning was about setup and workspace management. `internal/autopilot` runs
scheduled rituals inside a live IDE, but nothing runs a task from CI or a script, and the repo ships
no Action (`.github/workflows` has only `ci.yml` and `release.yml`). Approvals, scopes and the OS
sandbox (ADR-0019, ADR-0044) assume a human who can answer a card.

## Options
1. **Stay TUI-only.** Keeps ADR-0004 verbatim. CI users script the agent CLIs directly and lose DHI's
   scopes, budgets, worktrees and review.
2. **`dhi run <task>`: one more subcommand over the same services.** Not a second product: it
   composes the services `runTUI` composes (workspace, runtime, budget, worktrees) without Bubble
   Tea, prints a transcript or `--json` events, and exits with a code. Plus a thin Action.
3. **A separate `dhi-ci` binary.** Keeps `dhi` minimal, but doubles release, signing and docs.

## Recommendation (the user decides): option 2, with an ADR-0004 amendment
- Amendment: the binary may also expose `run`, whose only job is to execute an existing task or
  workflow headlessly. No setup, project or workspace management subcommands are added.
- No human, so no approval cards: every card resolves to deny unless the invocation names the grant
  explicitly (bounded by the manifest's scopes). Denial and budget stops (ADR-0031) have distinct
  exit codes. Nothing prompts or hangs.
- ADR-0011 holds: the OS sandbox stays a hard requirement; a runner without the helper refuses with
  the fix. An explicit, printed `--sandbox off` is the labeled opt-out.
- Missing tools are not installed in CI; the run fails naming the install command (ADR-0044).
- Output: a stable `--json` event stream plus exit codes. The Action, not DHI, opens PRs, using the
  runner's own credentials.
- The Action is a composite action in a separate repo that fetches the release asset for a pinned
  version, verifies its checksum and runs `dhi run`. It runs on the CI runner, not the user's
  machine, so ADR-0044's rule about the user's system is not engaged; it never pipes a remote script.

## Consequences
- "One interaction model" weakens to "one set of services, two front ends"; headless paths must be
  covered by the same service tests, not copies.
- CLI auth must be supplied as each CLI expects (secrets in CI). Untrusted PR content is fenced
  (ADR-0046) and write grants default off for forked PRs.

## If accepted
1. `cmd/dhi`: `run` subcommand (argument parsing only); `internal/headless` (new): compose services,
   event printer, exit-code table.
2. `internal/boot`: a non-interactive launch policy row (no setup gate, strict failures).
3. `internal/agentkit/scopes` and the approvals queue: a non-interactive resolver (deny or explicit
   grants).
4. `internal/doctor`: a headless readiness check (reuses `--json`).
5. `.github/workflows/release.yml` publishes checksums; the Action repo; spec F-129; ADR-0004
   amended by reference.
