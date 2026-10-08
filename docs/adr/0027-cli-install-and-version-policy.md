# ADR-0027: Confirm-gated CLI install into a DHI-owned folder; same-major version drift warns

Date: 2026-10-08 · Status: accepted · Supersedes in part: ADR-0012 §2
("user-owned, DHI never installs") and its exact-version rule · Companion to:
ADR-0005 (hermetic toolchain), ADR-0011 (no silent fallbacks). Serves: F-046.

## Context

ADR-0012 made host agent CLIs an opt-in, user-owned surface: DHI never
installs one and a missing binary is a named refusal. That kept DHI out of
vendor installers, but it leaves the most important first-run step — getting a
working coding CLI — to the user, with a doctor row as the only help. Separately,
adapters pinned an *exact* tested version, so the Claude CLI auto-updating from
2.1.177 to 2.1.285 turned `dhi doctor` red although nothing was wrong.

## Decision

1. **DHI may install a CLI, only after an explicit yes** that shows the exact
   command, the package and where it lands. Never silently, never as a fallback
   (ADR-0011 stands).
2. **Only through the toolchain seam.** CLIs distributed as npm packages
   (claude, codex, opencode, copilot) install with DHI's own pinned `npm`
   (`toolchain.Manager.NPMInstall`) into `<toolchain>/clis/<name>` with a private
   npm cache: no sudo, no global state, nothing outside the DHI prefix. CLIs whose
   only documented route is a vendor shell script (cursor) or whose route we could
   not verify (antigravity) are **guided-manual**: DHI shows the command and the
   vendor docs and re-detects; it does not pipe a remote script into a shell.
3. **Resolution order stays user-first.** `PATH` wins; the managed folder is
   consulted only when the binary is not on `PATH` (`clirun.ManagedLook`).
4. **The install catalog states only verified facts**, each with its source
   (vendor documentation), and is re-verified by a person when a pin is bumped.
5. **Authentication stays the CLI's own concern** (ADR-0012 §4). DHI shows how to
   sign in; it does not probe or store credentials.
6. **Version policy** (`clirun.Assess`): equal → ok; same major, different minor/
   patch → *warn* ("newer/older than verified"); different major → *fail*.
   Versions that do not parse compare as equal-or-different strings and fail only
   when different and unparseable. Date-versioned CLIs (cursor) use the year as
   the major.

## Consequences

- A first-time user can go from nothing to a working engine inside the wizard.
- Auto-updating CLIs stop breaking `dhi doctor` on every release, while a major
  bump (likely behaviour change) still fails loudly until re-verified.
- Guided-manual CLIs keep an honest limit: DHI does not run vendor scripts.
