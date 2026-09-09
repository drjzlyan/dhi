# ADR-0013: Remove the in-house anthropic engine; CLI runtimes only

Date: 2026-09-09 · Status: accepted · Companion to: ADR-0012 (host CLI
runtimes), ADR-0003 (provider seam — superseded here), ADR-0002 (native
runtime adapter seam — superseded here), ADR-0005 (hermetic), ADR-0011
(no silent fallbacks). F-013 is the implementation.

## Context

ADR-0003 built DHI's own turn engine: a narrow `provider.Stream` seam
with one Anthropic adapter, a tool round-trip loop inside the path-jail,
a policy gate feeding the editor's approvals panel, MCP bridges into a
native tool registry. ADR-0012 added host agent CLIs (Claude Code,
Codex, …) as an opt-in *second* runtime and left the in-house engine
untouched as the default.

Running two engines is double surface: two brain models, two tool
abstractions (native builtins vs. the CLI's own tools), two auth paths,
two approval sources. The in-house engine is also the only part of DHI
that reaches the network directly — everything else is hermetic
(ADR-0005). Keeping two engines doubles the attack surface and the
doctor surface for one fellowship. The fellowship's real, useful
identity has become: the IDE is the shell, the roster is the team, and
each agent thinks through a host CLI DHI drives, observes, and isolates.

## Decision

1. **The in-house engine is removed.** `internal/agentkit/provider`
   (the Anthropic stream seam + mock) is deleted, along with the native
   tool registry machinery (`Registry`, `Builtins`, `PolicyGate`,
   `RemoteTools`, `tools.Deps`, `tools.Call`, `tools.GitRunner`), the
   MCP *client* seam in the runtime, and the in-house turn loop
   (`prompt()`, tool round-trips). The `runtime` turn engine becomes a
   thin CLI dispatcher: every rostered agent runs through a registered
   host CLI, sandbox-wrapped (ADR-0012).
2. **`runtime` is now required and CLI-only.** A manifest without a
   registered `runtime` value is a parse error naming the valid set.
   The reserved `""` default and `"anthropic"` are gone — there is no
   implicit engine to fall back to.
3. **Approvals stay, but the in-house gate is gone.** The editor
   approvals panel and queue remain. The forward path is to surface the
   host CLIs' *own* permission prompts as approvals (the adapter's
   `--permission-mode` negotiation; a future adapter feature). Nothing
   enqueues approvals until then; the panel simply renders an empty
   queue.
4. **The manifest `tools` allowlist stays as intent, not execution.**
   For a CLI agent the tools live inside the host CLI; the allowlist no
   longer registers anything. It is retained (and still validated for
   well-formedness) as the declared set of *IDE-exposed* features the
   agent may call, which the future IDE-tool bridge (an MCP *server*
   DHI runs, serving search/git/review/etc. as tools) will honor.
5. **`policy_json` stays, scoped to sandbox roots.** Its rules no longer
   feed a tool gate; they configure the OS-sandbox root policy wrapped
   around every CLI spawn (ADR-0012 §3 boundary unchanged).
6. **Doctor drops the engine row.** `agents/api_key` (the in-house key
   warn) is removed; authentication is each CLI's own concern via its
   declared pass-through (`runtime/<cli>` rows already cover it).

### Consequences

- Env credentials: only the CLIs' declared pass-through (ADR-0012 §4)
  ever reaches an agent; the canonical `ANTHROPIC_API_KEY` is claude's
  concern, not DHI's.
- `internal/mcp` is currently unreferenced by production code; it
  survives because the IDE-tool bridge will grow its server side on it.
- The bus chat, reviewer, and ideator workflows are unaffected: they
  dispatch through the same `Runtime.Handle` seam, which now always
  spawns a CLI.
- Tests that scripted the engine via `provider.Mock` are rewritten
  against fixture CLI stubs on a fake PATH.