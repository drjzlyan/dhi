# F-035: Depth & cohesion — dashboard, co-editing, Slack, live work log

Status: planned (M20) · Companion to: ADR-0019 (DHI owns the loop),
F-021 (board), F-022 (slack floor), F-026 (UI beauty). Product rule: the
surface is the product; both the human and the crew should feel the IDE
respond.

## Summary

With the engine, tools, workflows, cross-project work, ideation, and
registry in place, F-035 closes the experience gap: the board becomes
Jira-grade, the editor becomes a true shared workspace, channels become
Slack-grade, and a task shows its agent's work as a live chat stream
rather than a post-hoc replay.

## Part A — board (Jira depth)

- Labels, priority, epic/parent links, due dates, filters and cross-card
  search, bulk operations, and swimlanes. Backlog vs. active remains the
  status model unless a sprint model is separately specified.

## Part B — editor co-editing

- Agents drive editor/LSP through the F-030 tools: `editor_open/reveal/
  apply_edit`, LSP hover/definition/references/rename/code-action. A
  human-visible indicator shows when an agent is editing a buffer;
  conflicts resolve through the existing undo-group/WorkspaceEdit path.

## Part C — Slack depth

- Channel message search, reactions, message edits, pins, per-message
  actions, and presence/working indicators. Typing indicators only if
  they can be made truthful (no fake state).

## Part D — live work log

- A task's agent work streams into its bound thread as chat while the
  run happens (the executor already emits per-event messages) — phases
  and tool calls visible live, not only in the post-hoc replay. The run
  replay remains for the durable record.

## Acceptance criteria

- [x] A card carries labels/priority/epic/due and can be filtered and
      bulk-moved. (Task schema 2; board `L`/`P`/`E`/`D`, `/` filter over
      title/slug/labels/epic/assignee, `space` mark + `M` bulk move.)
- [ ] An agent applies an editor edit the human sees applied, with an
      active-editing indicator.
- [x] Channels support search, reactions, and edits. (Chat pane `/`
      search, `+` reaction picker, `e` edit own messages, `p` pin, over a
      `channelmeta` store that leaves the bus JSONL immutable.)
- [x] A running task shows its agent's progress live in the thread.
      (`Runtime` tracks in-flight turns per thread — `Working` — and the
      board marks the bound card with a working glyph and the detail pane
      with a live line; the runtime already streamed progress/command/error
      events into the thread, replay remains the durable record.)
- [x] `make verify` green per phase; goldens regenerated deliberately.

## Deferred

- Sprint model (backlog/sprint planning), story points.
- Attachments/file uploads.
