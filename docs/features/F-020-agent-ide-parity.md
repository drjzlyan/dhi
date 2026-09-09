# F-020: Full IDE parity — the agent tool seam

Status: planned (M10 P3) · Milestone: M10 · Depends on: F-007
(runtime), F-013 (CLI runtimes). Inspired by: the user goal — "the IDE
tools should be available to the agents for everything: editing,
reading files, suggesting changes, chatting in channels, chatting with
the user — almost everything agents and humans interact with in the
IDE to complete the work."

## Summary

Today agents can already do much of what a human does in the IDE —
read/write/list/search files, commit/push, chat in channels and DMs,
and surface suggestion blocks — but the coverage is implicit and the
work-facing actions (task cards, PRs) are human-only. F-020 makes the
parity explicit: a documented matrix, and a DHI-side tool bridge so
host-CLI agents can invoke DHI actions the same way humans click
them, allowlisted per agent and audited like every tool.

## Part A — the parity matrix (documentation as contract)

| human in the IDE | agent capability | status |
|---|---|---|
| read / edit files | `read` `write` `list` `search` builtins in the sandboxed worktree | exists |
| commit / push | `git_commit` `git_push` | exists |
| chat in channels, threads, DMs | native bus posts inside turns | exists |
| suggest changes (chat block, ^f applies) | fenced suggestion blocks in replies | exists (documented here as the contract) |
| create / edit task cards | `task_create` `task_status` `task_assign` | NEW |
| open a PR for a task branch | `pr_open` (reviewSvc.CreatePRForBranch; gh shim missing → named refusal) | NEW |
| approve tool requests | native `tools.Approvals` Ask | exists |

Agent-to-agent and agent-to-human interaction parity is complete once
the NEW rows land; anything not in the matrix is out of scope for the
tool seam (the OS sandbox boundary stays the boundary, ADR-0012).

## Part B — the tool bridge

Host CLIs run their own tools inside the OS sandbox; DHI-namespaced
tool calls are intercepted by the clirun adapters' existing event
parsers (`tool_use` streams) and routed to a new `toolbridge.Seam`:

- A call is recognized when its name is a DHI builtin (`task_*`,
  `pr_open`, or the existing file/git set for audit symmetry).
- The manifest's `tools` allowlist gates every call — an
  un-allowlisted DHI tool is refused with the name, exactly like the
  CLI's own tool gating.
- Mutating ops (`task_*`, `pr_open`) require approvals (`tools.
  Approvals.Ask`) — the same y/n seam humans already answer.
- The bridge executes against the real stores (tasks, review) with
  strict validation; results return to the CLI as tool results so the
  agent can continue its turn.

## Acceptance criteria

- Manifest enum: new builtins validate (unknown tool named, strict
  round-trip); existing manifests unaffected (default allowlist none).
- Bridge: a fixture CLI emitting `dhi:task_create` produces a task
  card (store asserted); un-allowlisted → named refusal in the
  transcript; mutating op without approval parks in Approvals and
  resolves with y/n; `pr_open` without the gh shim refuses by name.
- Parity matrix lives in this doc and in doctor's runtime docs
  pointer; adapters' fixture tests cover the interception for at
  least claude + codex.
- `make verify` green.

## Deferred

- Interactive tools that need a human-present UI beyond approvals
  (e.g. agents driving the editor cursor).
- Cross-workspace agent actions.
- MCP-style third-party tool registries (DHI's ADR-0005 hermetic rule
  makes this a deliberate non-goal until a design exists).
