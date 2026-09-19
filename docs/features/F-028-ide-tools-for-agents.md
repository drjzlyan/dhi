# F-028: IDE tools for agents (the invisible capability)

Status: planned (M14 P0 accepted 2026-09-19)
Companion to: ADR-0017 (internal tool-serving substrate), ADR-0013
§4 (the reserved MCP-server path), F-020 (IDE parity, toolbridge
predecessor), ADR-0004 (minimal CLI), ADR-0011 (no silent fallbacks).
Product rule: the user sees the capability — "agents can use IDE
tools" — never the mechanism. Everything is configured in the IDE;
there is nothing to set up.

## Summary

Agents get mid-turn access to DHI as tools: task cards, the knowledge
base, their own memory, and their channels. The runtime wires this
automatically for every supported coding CLI; the only visible knobs
are the manifest `tools` allowlist (already in the agent form) and the
approvals prompts (already the human's y/n). The helper runs as a
short-lived child of the existing agent spawn, excluded from the
product's CLI surface and help text.

## Part A — the tool surface (user view)

- Agents can, mid-turn: list tasks and read cards; create a task;
  change a task's status; assign a task; search the knowledge base;
  contribute to the knowledge base (review-gated, as today); append to
  their memory journal and read/write their notes; read a channel's
  history and post to it; and run workspace search (ripgrep fan-out,
  read-only).
- Git stays CLI-native inside the sandbox (no duplication).
- Every tool is gated by the manifest `tools` allowlist named by slug;
  mutating tools cross `tools.Approvals.Ask` (the human's y/n) —
  refusals reach the agent's turn as named results, never silence.
- Results land in the same thread the user watches; transcripts keep
  recording tool traffic (F-014 observability unchanged).

## Part B — mechanism (one implementation note)

- ADR-0017: the runtime spawns `dhi __toolserve` (hidden subcommand,
  not in usage/help) per turn as a stdio MCP server child; adapters
  receive generated config automatically (claude/codex/opencode
  first-class; cursor/copilot/gemini via config injection, live-verify
  checklist in each adapter).
- The stores opened by the helper are the same on-disk stores the TUI
  opens; freshness follows the existing per-frame aggregation +
  atomic-write discipline.
- `dhi-action` final-message blocks are deprecated: kept one release
  as fallback parsing, then removed.

## Acceptance criteria

- [ ] An agent with the right allowlist can, inside one turn: read
      open tasks, create one, mark another in-review, search the KB,
      and post progress to its channel — with each mutation surfaced
      as an approval the user answers.
- [ ] A manifest without the tool names gets named refusals (the tool
      is not offered, and the agent is told what is missing).
- [ ] No new user-facing configuration exists: zero flags, zero env,
      zero config files. `dhi doctor` gains an `agent-tools` health
      row (ok / not available with the reason named).
- [ ] The hidden subcommand never appears in `usage:` output or help.
- [ ] Tool calls are visible in the run transcript replay (F-014).
- [ ] `make verify` green per phase.

## Deferred

- File read/write/list as served tools (agents keep the sandboxed CLI
  tools for the filesystem).
- GUI-action tools (opening editors/panes); MCP over HTTP between
  DHI instances.
