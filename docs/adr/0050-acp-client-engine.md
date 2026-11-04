# ADR-0050: An ACP (Agent Client Protocol) transport for CLI engines

Date: 2026-11-04 · Status: proposed · Companion to: ADR-0019 (CLI is the engine), ADR-0024 (no
direct-API engine), ADR-0018 (loopback serving), ADR-0044, ADR-0043
Serves: gap G-C10.

## Context
**What ACP is.** An open protocol, started by Zed, that standardises how an editor talks to a coding
agent: JSON-RPC over stdio to a locally spawned agent subprocess (HTTP/WebSocket for remote agents
is still in progress). The client calls `initialize`, creates or resumes a session, sends prompts
and receives streamed session updates (message chunks, tool calls, plans, diffs). The agent can ask
the client for permission, and for file and terminal access (`fs/*`, `terminal/*`) if the client
advertises them. The client passes MCP servers to the agent at session creation, and content reuses
MCP's JSON shapes. Stable v1 and a draft v2 exist; there is a public registry of 50+ agents.
**Who speaks it** (from the ACP site and secondary sources, to be re-verified before building):
Gemini CLI natively; GitHub Copilot CLI (since early 2026); Cursor (its CLI documents an `acp`
mode); opencode; Goose, Kiro and others. Claude Code and Codex are reached through separate adapter
programs (usually fetched with npx), not by the vendor binaries themselves.
**In DHI today.** `internal/agentkit/clirun` has one adapter per CLI, each with its own argv and
stream parser. No raw-completion mode exists (ADR-0019), and ACP agents are still agent loops, not
bare models, so ADR-0024 (no direct-API engine) is not touched: the engine remains a vendor CLI
subprocess.

## Options
1. **Do nothing.** Keep six hand-written parsers; every CLI format change is DHI's maintenance.
2. **ACP as a second transport for the existing `cli:<name>` engine.** An adapter may declare
   `transport = acp` with the argv that starts it. DHI is the client: it advertises *no* `fs` or
   `terminal` capability (the agent has to use DHI's tools), passes the loopback MCP endpoint
   (ADR-0018) in session setup, maps `request_permission` to the approvals queue, and renders
   session updates through the existing event path.
3. **ACP-first: replace the parsers.** Biggest payoff, but the vendor CLIs that need a third-party
   npx adapter would make ACP a download path (ADR-0044), and ACP v2 is still draft.

## Recommendation (the user decides): option 2, one pilot
- Pilot on a CLI whose vendor binary speaks ACP itself (Gemini CLI or Copilot CLI), behind the MCP
  verification gate ADR-0019 §2 already requires. Adapters that need a separate bridge program are
  not shipped; if a user has one, it is a host tool they install through an approval card.
- The engine name does not change (`cli:gemini`); `transport` is an adapter property, so there is
  no new engine kind and ADR-0024's "exactly one kind" stands.
- Because the client advertises no fs/terminal, an ACP agent's native file and shell tools stay what
  they are today for other CLIs: best-effort containment (ADR-0019 §5), still sandboxed and still
  named in doctor. ACP does not close that gap.
- Fall back to the adapter's normal stream parser only as an explicit setting, never silently
  (ADR-0011): an ACP handshake failure is a named doctor failure.

## Consequences
- Structured events, cancel and resume replace regex-grade parsing for ACP-capable CLIs; one
  permission path.
- A second transport to test (a fake ACP agent in `testutil`) and a protocol version to track.
- Cost and usage reporting differ per agent; the budget ledger (ADR-0031) must handle "unknown".

## If accepted
1. Research spike: confirm launch flags, v1 method names and MCP passing against the pilot CLI.
2. `internal/agentkit/acp` (new): JSON-RPC stdio client, capability negotiation, update mapping.
3. `internal/agentkit/clirun`: `transport` field on the adapter declaration; ACP path in the runner.
4. Approvals queue: map permission requests to cards; `internal/agentkit/mcpbridge`: pass loopback.
5. `internal/doctor`: ACP handshake row; fake-agent tests; spec F-130.
