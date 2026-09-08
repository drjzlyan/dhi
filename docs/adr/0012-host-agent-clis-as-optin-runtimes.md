# ADR-0012: Host agent CLIs as opt-in runtimes

Date: 2026-09-08 · Status: accepted · Companion to: ADR-0003 (provider
boundary), ADR-0005 (hermetic toolchain), ADR-0006 (path-jail/sandbox),
ADR-0011 (no silent fallbacks); F-013 is the implementation.
Inspired by: Multica's daemon-runtimes model — "drives them, doesn't
ship them" (https://github.com/multica-ai/multica).

## Context

ADR-0003 made the LLM boundary a narrow `provider.Stream` seam with a
single Anthropic adapter: every rostered agent thinks through DHI's own
turn engine. That is one kind of brain. Users also run agent CLIs —
Claude Code, Codex, OpenCode, and many more — which are *autonomous*
agents: give them a task and a directory, and they do the work
themselves. Multica built a whole platform on orchestrating exactly
these, and the idea lands naturally on DHI's existing machinery
(roster, org, tasks↔worktrees, bus, reviewer, OS sandbox): the team
exists, only the kind of teammate is new.

Two DHI invariants look like they block this:

- ADR-0005 says DHI must run hermetic and must not shell out to
  unmanaged tools outside the `internal/toolchain` seam.
- ADR-0011 says nothing may degrade or leak silently — no ambient
  environment, no invisible fallback.

Neither blocks this; both need a *named* category. The CLIs are
user-owned: DHI cannot digest-pin them (proprietary installers, user
authentication, vendor update cadence), and their absence must never
block boot. So they are a declared opt-in surface, not part of the
hermetic toolchain, and every property of using one is explicit and
doctor-visible.

## Decision

1. **A declared runtime category.** Agent manifests gain
   `runtime = "" | "anthropic" | <registered cli name>` (default `""`
   = today's behavior, unchanged). A new `internal/agentkit/clirun`
   registry declares, per CLI: binary name, tested version range,
   headless invocation builder, structured-stream parser, cost
   extractor, and the **exact environment pass-through list**.
   Manifest validation accepts only registry names (strict, names the
   valid set); an unknown name is a parse error, not a fallback.

2. **User-owned, not DHI-managed.** ADR-0005 continues to govern the
   tools DHI ships (git/node/rg/uv/gh stay registry-pinned shims).
   Host CLIs are resolved from the user's PATH at boot — the one
   sanctioned host lookup for this category — and DHI never
   auto-installs one. A missing CLI is a named refusal at use plus a
   doctor row (Warn when no agent needs it, Fail when a rostered agent
   does). This is ADR-0005's exception, made explicit and table-tested
   in the registry rather than left to code archaeology.

3. **The OS sandbox is the boundary.** Every CLI spawn is wrapped by
   the OS-sandbox adapter (seatbelt/bubblewrap) through the existing
   `Sandbox` seam, with per-CLI declared rw roots (the CLI's own state
   dir, the task worktree) alongside the standard system allows. The
   CLI's *internal* approval prompts are disabled (Claude Code
   `--permission-mode bypassPermissions`, Codex
   `--dangerously-bypass-approvals-and-sandbox` — a flag explicitly
   intended for "environments that are externally sandboxed"); the
   *outer* OS sandbox is on. Network stays policy-engine territory as
   in F-010.

4. **Environment is declared, never ambient.** Each adapter lists the
   env vars it passes (HOME, XDG_*, CLI-specific) in code; the runtime
   assembles exactly the hermetic toolchain PATH plus that set —
   nothing else crosses (ADR-0011 spirit). Doctor reports the
   effective env set per CLI so the declaration is auditable.

5. **Runs, not turns.** A CLI engagement is one delegated execution:
   task prompt → autonomous work in the attached worktree → transcript
   + usage + result. It is recorded as a `run` on the task card,
   streams progress into the bound bus thread, and obeys an explicit
   timeout + retry policy (default: no retries, hard timeout; per-agent
   override). Failure stops with the reason posted — never silent.
   The in-house Anthropic provider path is untouched and remains
   first-class; both runtime kinds share roster/org/tasks/review/
   standards, and both record into one run schema (F-014).

## Consequences

- Agents can be powered by any model behind the CLIs; those agents
  need no DHI provider key.
- A new external trust surface exists (CLI binary + vendor updates).
  Mitigations: tested version ranges pinned in the registry, doctor
  version reporting, the OS sandbox, declared env.
- Seatbelt/bubblewrap profiles grow per-CLI rw roots; F-010's
  system-allow gotchas apply to the new roots.
- `runtime.Turn` gains one branch point; ADR-0003's boundary is intact
  for in-house agents.
- CLI headless flags drift across vendor releases: adapters isolate
  the drift (one file per CLI, fixture-tested), and the registry pins
  the tested version range so a vendor change is a visible doctor
  mismatch, not a silent behavior change.
