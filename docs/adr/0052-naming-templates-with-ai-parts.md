# ADR-0052: Naming and message templates with AI-written parts

Date: 2026-11-05 · Status: accepted (user request, session 71) · Companion to: ADR-0011 (no silent
fallback), ADR-0044 (the user's system is read-only), ADR-0046 (untrusted fence), ADR-0051 (one-shot
read-only calls)
Serves: F-182.

## Context
Users want to define their own branch names, commit titles and bodies, and PR titles and
descriptions. They want to combine fixed text, symbols DHI knows (ticket, slug, user, …) and parts an
AI writes. The result has to be the same whichever of the six CLIs the agent runs on. DHI's existing
placeholders were fixed per field and could not mark a part as AI-written.

## Decision
1. **One template language**, in the pure `conventions` package, used by every field: `{symbol}`,
   `{symbol|filter}`, `{ai}`/`{ai:hint}`, and `{{ }}` for literal braces. Each field has an explicit
   symbol list. `{ai}` is refused where the result must be deterministic (review branches, worktree
   folders).
2. **DHI renders; the AI writes only the slots.** When an agent is acting, it passes slot text
   (`fill`) to the MCP tools, or writes the whole text, which DHI checks against the template with the
   task's own values. Every CLI gets the same tools and the same prompt section, so behaviour does not
   depend on the CLI.
3. **When no agent is acting**, DHI asks the default agent in one read-only call, the same way
   `:suggest` does (ADR-0051): fenced input, metered, nothing posted. CLIs without a read-only mode are
   refused by name. DHI never silently falls back to a non-AI value.
4. **One PR rendering path** in the review service, used by every caller, rendering before the push.
5. **No `commit-msg` hook.** Gating an agent's own shell `git commit` would mean writing into member
   repos or overriding `core.hooksPath`, which ADR-0044 forbids and which would clash with the user's
   own hooks. The prompt tells agents to commit only through `git_commit`, and the default branch stays
   guarded by the sandbox (F-086).

## Consequences
- Each template can be checked by itself, and Settings refuses a template that cannot render.
- A human's commit or PR whose template has AI parts costs one short agent call, recorded in the
  ledger.
- A team whose default agent runs on a CLI without a read-only mode writes AI parts by hand.
