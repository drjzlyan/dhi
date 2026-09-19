# ADR-0017: Internal tool-serving substrate — hidden helper, invisible plumbing

Date: 2026-09-19 · Status: accepted · Serves: F-028.
Bounds: ADR-0013 §4 (the reserved MCP-server path), ADR-0012 (host
agent CLIs as opt-in runtimes), ADR-0004 (minimal CLI), ADR-0011 (no
silent fallbacks), ADR-0005 (hermetic).

## Context

Run-to-completion CLIs cannot receive mid-turn results through the
neutral stream-event model, so F-020 shipped the ```dhi-action```
final-message bridge: one code path for all six adapters, but no
mid-turn tools. ADR-0013 deliberately kept `internal/mcp` (client-
only) as the substrate for a future MCP *server* exposing DHI ops to
agent CLIs — every supported CLI speaks MCP. The product rule (M14):
the user configures everything in the IDE and never sees the
mechanism, and the CLI stays ops-driven.

## Decision

1. **Server side grows on the kept substrate.** `internal/mcp` gains
   inbound request handling (initialize, tools/list, tools/call) on
   the existing conn codec/transport (stdio, newline-framed). The
   `mcp__<server>__<tool>` manifest validation finally resolves.
2. **The helper is this binary, hidden.** The runtime spawns the DHI
   executable as `dhi __toolserve` — a stdio MCP child per agent
   turn. The subcommand is excluded from usage/help output and from
   doctor's notion of the CLI surface; it is plumbing, not product
   (ADR-0004: the visible CLI stays ops verbs only). No daemon, no
   listening port — the helper lives for the turn and exits (ADR-0012
   spirit: no long-running processes).
3. **Adapter wiring is automatic and silent.** The runtime generates
   per-adapter MCP configuration (temp config file or CLI flags) and
   injects it into the spawn — claude/codex/opencode first-class;
   cursor/copilot/gemini via their documented config paths with a
   live-verify checklist per adapter (fixture-first until then, the
   M8 wave-3 rule). Nothing lands in the user's real CLI configs.
4. **The tool surface is the domain layer, allowlist-gated.** The
   helper opens the same on-disk stores (tasks, knowledge, memory,
   bus) in read/write; every tool slug must appear in the manifest
   `tools` allowlist or the tool is absent from tools/list; mutating
   tools cross `tools.Approvals.Ask` exactly as the toolbridge did —
   the approvals queue the user already answers.
5. **Cross-process discipline.** Helper and TUI open the same files;
   correctness rides the existing store rules (atomic writes,
   strict decode, subscribe-on-open) and per-frame aggregation in the
   TUI. Concurrent writers on one store file are last-writer-wins at
   file granularity — the same accepted discipline as two DHI
   instances on one workspace; documented, not invented here.
6. **dhi-action is deprecated, not deleted.** Final-message block
   parsing stays as a fallback for adapters without verified MCP
   support; its prompt contract stops advertising once MCP covers an
   adapter. Removal is a later hygiene pass with the tests migrated.
7. **Doctor sees health, not mechanism.** A single `agent-tools` row
   reports ok / not-available-with-reason. No user-facing flags, env,
   or config files exist for this feature.

## Consequences

- `cmd/dhi` gains an unexported-usage subcommand branch (the only
  CLI change; usage text unchanged for users).
- The toolbridge's four actions keep their semantics as MCP tools;
  its tests migrate or run in fallback mode.
- Transcripts record tool traffic through the existing event stream
  (F-014 replay shows tool use).
- Sandbox: the helper child is spawned wrapped like any other spawn —
  same seatbelt/bubblewrap path, same system-allow rules (gotcha 23).
- A `tools` allowlist without the new slugs behaves exactly as today:
  the agent gets a named refusal describing what is missing.
