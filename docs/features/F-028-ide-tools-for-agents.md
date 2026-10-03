# F-028: IDE tools for agents (the invisible capability)

Status: implemented (M14 P3, 2026-09-24). ADR-0018 supersedes ADR-0017
§2–3: serving runs in-process over a per-turn loopback endpoint, not a
spawned helper (approvals stay the one queue the TUI renders).
Companion to: ADR-0017 (internal tool-serving substrate), ADR-0018
(loopback decision), ADR-0013 §4 (the reserved MCP-server path), F-020
(IDE parity, toolbridge predecessor), ADR-0004 (minimal CLI), ADR-0011
(no silent fallbacks). Product rule: the user sees the capability —
"agents can use IDE tools" — never the mechanism. Everything is
configured in the IDE; there is nothing to set up.

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

- ADR-0018: the runtime serves the tools from the DHI process over a
  per-turn `127.0.0.1` loopback MCP endpoint (`mcp.ServeLoopback`) and
  hands MCP-capable adapters a generated temp config (claude:
  `--mcp-config … --strict-mcp-config`). The endpoint + config live
  exactly as long as the turn; no daemon, no child process (ADR-0012).
- The stores the handler operates on are the same in-process instances
  the TUI opened — approvals are the one queue the human answers, and
  results post to the trigger thread.
- `mcp.ServeStdio` is kept in reserve for adapters whose only verified
  MCP wiring is a spawned stdio server; no `dhi` subcommand is added
  (the visible CLI surface stays ops verbs only).
- `dhi-action` final-message blocks are deprecated: kept one release as
  fallback parsing for adapters without verified MCP wiring, then
  removed.

## Acceptance criteria

- [x] An agent with the right allowlist can, inside one turn: read
      open tasks, create one, mark another in-review, search the KB,
      and post progress to its channel — with each mutation surfaced
      as an approval the user answers (`internal/agentkit/dhitools`
      round-trip + loopback tests; the runtime wires it to the spawn).
- [x] A manifest without the tool names gets named refusals (the tool
      is not offered in `tools/list`, and an unlisted call is refused
      by name; the `dhi-action` fallback names the allowed set).
- [x] No new user-facing configuration exists: zero flags, zero env,
      zero config files. `dhi doctor` gains an `agent-tools` health
      row (ok / warn with the reason named).
- [x] The hidden subcommand never appears in `usage:` output or help
      (ADR-0018 dropped the subcommand entirely; usage unchanged).
- [x] Tool calls are visible in the run transcript replay (F-014) —
      MCP traffic rides the adapter's existing event stream.
- [x] `make verify` green per phase.

## Deferred

- ~~File read/write/list as served tools~~ — **landed** (M15 P1).
- GUI-action tools (opening editors/panes); MCP over HTTP between
  DHI instances.
