# ADR-0018: Tool serving over an in-process loopback endpoint

Date: 2026-09-24 · Status: accepted · Supersedes: ADR-0017 §2–3 (the
`dhi __toolserve` stdio helper process). Serves: F-028.
Bounds: ADR-0017 (the kept MCP server substrate), ADR-0013 §4, ADR-0012
(no long-running processes), ADR-0004 (minimal CLI), ADR-0011.

## Context

ADR-0017 chose a hidden `dhi __toolserve` stdio child per agent turn as
the mechanism for serving DHI's IDE tools. During F-028 implementation
the approvals requirement forced a re-think: every mutating tool must
cross `tools.Approvals.Ask`, and that queue is an in-memory instance the
TUI answers. A separate helper process would need its own approvals
instance or a new cross-process queue — either way the human's y/n
would not reach the surface the user is already looking at, and the
"results land in the thread the user watches" contract would break.

## Decision

1. **The server runs in the DHI process; the adapter dials it.** The
   runtime starts `mcp.ServeLoopback` — a streamable-HTTP MCP endpoint
   bound to an ephemeral `127.0.0.1` port — for the duration of one
   turn and stops it when the turn ends. No daemon, no child process
   (ADR-0012 spirit preserved).
2. **Adapter wiring is unchanged in shape.** The runtime writes a temp
   MCP config naming the loopback endpoint and passes it to MCP-capable
   adapters (claude first-class: `--mcp-config … --strict-mcp-config`,
   keeping the user's own MCP servers out of the run). The config file
   dies with the session.
3. **Approvals stay single-instance.** Because serving is in-process,
   `tools.Approvals.Ask` is the exact queue the TUI renders; y/n reaches
   the human's surface with no new plumbing.
4. **The stdio transport is kept in reserve.** `mcp.ServeStdio` and the
   `Handler` seam remain for adapters whose only verified MCP wiring is
   a spawned stdio server. No `dhi` subcommand is added: the visible CLI
   surface stays ops verbs only (ADR-0004), and nothing new appears in
   usage/help.
5. **Fallback is per-adapter and explicit.** Adapters without a
   verified MCP wiring (`MCPOK=false`) keep the deprecated `dhi-action`
   final-message bridge advertised; MCP-capable adapters get the tools
   contract through `tools/list` and the bridge advertisement is
   suppressed for that turn.

## Consequences

- `internal/mcp` gains the inbound handler + loopback transport; the
  loopback HTTP client also tolerates `202 Accepted` for notifications
  (the spec's notification reply).
- `doctor` reports one `agent-tools` row (ok / warn-with-reason); no
  user-facing flags, env, or config files exist.
- Sandbox: the endpoint is loopback and the seatbelt profile already
  allows network (policy-engine territory, ADR-0006); the CLI reaches
  `127.0.0.1` without new roots.
- The `dhi-action` removal remains a later hygiene pass once every
  targeted adapter has verified MCP wiring.
