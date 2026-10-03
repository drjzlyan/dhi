# F-020: Full IDE parity — the agent tool seam

Status: implemented (M10 P3, 2026-09-09)
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

## Part B — the tool bridge (as built)

The neutral stream-event model carries no structured tool args, and a
run-to-completion CLI cannot receive mid-turn results — so interception
happens at the turn boundary, on the same channel agents already use
for suggestions: **```dhi-action blocks in the final message**.

````
```dhi-action
{"action": "task_create", "args": {"slug": "fix-login", "title": "Fix login race"}}
```
````

The runtime parses the final summary, and the `toolbridge.Bridge`
executes each request:

- **Allowlist gate** — every action must be in the manifest's `tools`
  (the bridge actions are builtins: `task_create`, `task_status`,
  `task_assign`, `pr_open`); an un-allowlisted action refuses with the
  name.
- **Args are strict** — unknown keys refuse with the key named; bad
  values (statuses, unknown slugs) refuse naming the value.
- **Approvals gate** — every mutating action crosses
  `tools.Approvals.Ask`, the same y/n seam humans already answer; a
  denial refuses and writes nothing.
- **Results land in the thread** — result or named refusal posts to
  the trigger's channel + thread, so the agent sees the outcome on its
  next turn. Malformed blocks refuse per-block; well-formed ones still
  dispatch.
- **Prompt-side contract** — when an agent's allowlist includes bridge
  actions, the system prompt carries the block shape and the valid
  names; agents can only request what they are allowed to.
- **pr_open** resolves the task card's first changeset (member +
  branch) and opens through the review service; gh missing or no
  worktree refuses by name.

The stream-interception point named in the earlier draft is a
documented deviation: adapters would need per-CLI structured tool-event
plumbing and a mid-turn result channel that run-to-completion CLIs do
not offer. The turn-boundary design keeps one code path for all six
adapters.

## Acceptance criteria

- Manifest enum: new builtins validate (unknown tool named, strict
  round-trip); existing manifests unaffected (default allowlist none).
- Bridge: a fixture CLI emitting a `dhi-action` task_create block
  produces a task card (store asserted) and the result posts to the
  thread; un-allowlisted → named refusal in the thread, nothing
  written; mutating op parks in Approvals and resolves with y/n;
  `pr_open` without the review seam or worktree refuses by name.
- Prompt contract: the system prompt carries the block shape exactly
  when the allowlist includes bridge actions (allowedActions test).
- Parity matrix lives in this doc; the bridge applies to every adapter
  through the one turn-boundary path (no per-CLI code).
- `make verify` green.

## Deferred

- Interactive tools that need a human-present UI beyond approvals
  (e.g. agents driving the editor cursor).
- Cross-workspace agent actions.
- ~~MCP-style third-party tool registries~~ — **closed (not planned)**:
  a deliberate non-goal (ADR-0005).
