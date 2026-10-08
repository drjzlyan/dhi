# F-036: Team collaboration — agent hand-offs, team lead, task hand-off

Status: complete (M21) · ADR-0025 · Companion to: F-022 (Slack floor),
F-021 (board), F-030 (tool surface).

## Summary

A team works by role: the human gives a team (or one agent) a task, the
lead triages, and agents hand work to each other with @mentions.

## Behaviour

- Agent replies and `channel_post` dispatch @mentions (ADR-0025 §1).
- Hop budget of 8 per channel since the last human message; the limit
  posts a visible notice.
- Ideation session channels are NOT runtime-routed: the floor protocol owns
  hand-offs there (ADR-0025 §5).
- A bare human post in `#<team>` goes to the team lead; no lead → named
  notice; human lead → quiet.
- Board `n` takes `assign` and `team`; creating or assigning a card posts
  the brief (bound thread → team channel → assignee DM) and dispatches.
- `task_create` accepts assignee/team/labels/priority; with both
  assignee and team it @mentions the assignee in `#<team>`.

## Acceptance

- [x] targets: mention beats lead; agent bare post routes nowhere;
      self/unknown mentions ignored (`runtime/dispatch_test.go`)
- [x] hop budget stops loops with a notice; human post resets; per channel
- [x] no-lead notice; human lead silent
- [x] board hand-off routing table (`workspace/handoff_test.go`)
- [x] `task_create` fields, hand-off post, relay, bad-priority refusal
- [x] real `Runtime` + stub CLI: an `@bo` in an ordinary channel spawns
      exactly two CLIs; the same message in `#ideation-*` spawns one

## Deferred

- Lead-authored decomposition into subtasks (the lead can already create
  tasks via `task_create`).
- Per-thread budgets; persisting hops across restarts.
