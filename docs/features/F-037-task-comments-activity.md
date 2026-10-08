# F-037: Task comments & activity trail

Status: complete (M22) · Companion to: F-021 (board), F-035 (board depth),
F-036 (hand-offs). Product rule: the card is the durable record; chat is
the live conversation.

## Behaviour

- Task schema 3 adds `[[comment]]` (author, text, at) and `[[activity]]`
  (kind status|assignee|priority|labels|run|comment, from, to, actor, at).
  Schema 1/2 cards load unchanged and upgrade on their next write.
- `Store.AddComment` (author must be a valid member id, text 1–4000 bytes).
  Setters record activity only when the value actually changes; runs
  append a `run` entry attributed to the agent. Activity keeps the newest
  200 entries; comments are never trimmed.
- `SetStatusAs` / `AssignAs` attribute the change; the served
  `task_status` / `task_assign` tools pass the calling agent.
- Served tool `task_comment` (Write scope, allowlist/approval as other
  task tools); builtin reviewer/fixer/planner roles include it.
- `Store.mutate` now serializes its read-modify-write so concurrent
  agents cannot overwrite each other's card writes.
- Board: `N` comments as the human. The wide side pane shows the newest 3
  comments and 5 activity entries; narrow widths show one summary line.

## Acceptance

- [x] persistence + activity kinds/actors, no-op moves record nothing
- [x] refusals: empty, bad author, over-length, unknown task
- [x] activity cap with newest kept; comments untrimmed
- [x] 12 concurrent comments all persist
- [x] schema-2 card loads and upgrades
- [x] `task_comment` tool + attribution; board `N` flow and detail render

## Deferred

- Editing/deleting comments; @mentions inside comments waking agents;
  a full-screen comment/activity viewer (the side pane shows the newest).
