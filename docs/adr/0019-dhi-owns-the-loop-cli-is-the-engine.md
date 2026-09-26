# ADR-0019: DHI owns the loop, the host CLI is the engine

Date: 2026-09-26 · Status: accepted · Supersedes: ADR-0012 (host
agent CLIs as opt-in runtimes), ADR-0013 (remove the in-house engine)
— their invariants are preserved, their ownership model is inverted.
Companion to: ADR-0005 (hermetic toolchain), ADR-0006 (path-jail /
sandbox), ADR-0011 (no silent fallbacks), ADR-0018 (loopback serving),
ADR-0020 (workflows), ADR-0021 (cross-project), ADR-0022 (trust).
Serves: F-030.

## Context

ADR-0013 made DHI a thin dispatcher: every rostered agent thinks
through a host CLI, and *the tools live inside the host CLI* — DHI
sends one flattened prompt and renders the CLI's already-executed
events (`runs, not turns`, ADR-0012 §5). The product goal has outgrown
that ownership model. The user's crew must work only through DHI's
declared IDE surface (editor, LSP, git, board, channels, ideation),
across multiple projects, under feature workflows and capability
scopes. Those are orchestration concerns that cannot live inside a
vendor CLI's private tool loop.

Each host CLI is an autonomous agent, and — verified against the six
adapters — **none exposes a raw-completion mode**: `claude -p`,
`codex exec`, `opencode run`, `cursor-agent -p`, `copilot -p`,
`gemini -p` all run the CLI's own tool loop. So "the CLI is the
engine" cannot mean "the CLI is a bare model"; it means **the CLI is
the inference + inner-reasoning engine, and DHI is the host that
declares the tools, the authority, the workflow, and the record.**

## Decision

1. **The engine seam is `engine = "cli:<name>"`.** The manifest names
   an engine; the workspace has a default engine; a per-agent override
   is allowed. The existing six adapters become engine declarations
   (binary, tested range, headless argv, stream parser, cost, env
   pass-through). An unknown engine name is a strict parse error, never
   a fallback (ADR-0011).
2. **DHI owns the tool surface.** Every IDE capability is served to the
   engine over the MCP loopback endpoint (ADR-0018): filesystem
   read/write/patch/list/glob, search, git, editor open/reveal/
   apply-edit, LSP, tasks, KB, memory, channels, ideation, board/PR/
   review reads, and an allowlisted `run`. **MCP wiring must be verified
   for every shipped adapter** before its engine may be selected (the
   `MCPOK` gate generalizes; `dhi-action` is removed once parity lands).
3. **Capability scopes replace the per-tool allowlist.** Authority is
   `read · write · exec · network · git · push · admin`, each mapped to
   `auto | ask | deny`, declared per agent and overridden by team then
   workspace policy. Every mutation crosses the one approvals queue;
   the human may answer `grant once` or `grant always` (grant-memory),
   and every decision is recorded in the run transcript.
4. **No free-form shell.** There is no `bash` tool. Developer power
   comes from structured tools plus an allowlisted `run` (build / test /
   lint / git subcommands declared by the workflow), executed under the
   OS sandbox. **Network is deny-by-default**; an agent may reach only
   the origins its scope and workflow declare. This is how "work only
   inside the IDE" becomes auditable rather than aspirational.
5. **Containment, not a hard guarantee — stated plainly.** A host CLI
   retains its own native tools; DHI suppresses them best-effort per
   CLI (deny flags where they exist + the system block). A hard
   guarantee requires the engine to be DHI's own tool loop, so a
   **direct-API engine kind (`engine = "api:<provider>"`) is designed
   but not built**; it is the escape hatch the day absolute scope or
   offline inference is required. Until then, `doctor` must not claim
   a guarantee it cannot keep.
6. **Preserved invariants (from 0012/0013).** CLIs stay user-owned
   (never auto-installed; named refusal at use; doctor row), environment
   stays a declared pass-through list (never ambient), the OS sandbox
   remains the outer boundary, and nothing degrades silently.

## Consequences

- `internal/agentkit/runtime` regains a real turn abstraction: DHI
  assembles context, resolves the workflow and scopes, drives the run,
  gates mutations, and records one run schema — the CLI is invoked, not
  obeyed.
- The adapter interface widens from "argv + parser" to "engine
  declaration + MCP capability"; `MCPOK=false` is no longer a shipping
  state.
- The `internal/mcp` outbound client is revived as the IDE tool server's
  counterpart, and the approvals queue becomes the authority ledger.
- Capability scopes are a manifest schema change (schema 3); schema-1/2
  files load with derived scopes, never a silent grant.
- The "only IDE tools" claim is documented as best-effort containment in
  product docs and doctor output; the API engine kind is the recorded
  path to a guarantee.
