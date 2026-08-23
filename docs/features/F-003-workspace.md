# F-003: Workspace surface — company of agents

Status: planned · Milestone: M4 (domain models land M1)

## Summary

The **boot view** of DHI (carries the centered brand hero until M4 fills it).
Slack-like operations floor where the human runs a small company of agents:
organize teams, converse, assign tasks, and inspect each agent's work,
memory, and knowledge.

## Components

1. **Channels** — `#general`, per-team channels, direct messages with any
   agent; threaded replies; @mention triggers agent turns (M3 runtime);
   task assignment happens conversationally and via explicit task cards.
2. **Organization** — create/edit/archive agents; form teams; assign agents
   to teams; define team leads (human or agent); roster views by team.
   Installing new agents = marketplace pack (local path/git URL).
3. **Task tracking** — kanban-ish list per team/channel: backlog/active/
   in-review/done; each task binds a ChangeSet (per-repo worktrees) and its
   thread; click-through shows what the agent is doing right now.
4. **Inspection** — per-agent profile: current task/worktree, recent
   activity timeline, private memory (journal + notes, read-only),
   contributions to shared KB, skill/tool inventory.
5. **Attach points** — any rostered agent is invitable into Editor chat
   sidebar, Ideator sessions, and Reviewer sessions (single source roster).
6. **Workspace management** — add/remove/rename member repos from the UI
   (ADR-0004: no PM subcommands in the shell). Add via local path or git
   URL (clone into `repos/`, then register). Alias validation follows
   `internal/workspace` rules; `.dhi/workspace.toml` is rewritten
   atomically; removal unregisters the member but never deletes a working
   tree without explicit confirmation. Live surfaces (editor tree,
   ripgrep fan-out roots, terminal tabs) re-resolve members without an
   app restart.

## Acceptance criteria

- Assign a task to a team; worker agent picks it up in its own worktree(s);
  progress visible as thread messages; completion flips task state.
- Inspect agent memory after a session; entries exist and are readable.
- Invite an agent from roster into an Editor chat session without leaving flow.
- Add and remove a member repo entirely from the UI; editor tree, search,
  and terminal tabs reflect the change without restarting DHI.
