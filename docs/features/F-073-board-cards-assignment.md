# F-073 — Board cards, comments, assignment

Status: done (M30 P2)

## Problem
The Jira-like board did not make it obvious who was working on a card.
Comments showed as plain wrapped text, only the last three. Assigning
meant typing a name. A human could not take a card and start editing in
one step, and handing a card to another agent did not tell it where the
earlier work was.

## Behaviour
- **Cards.** Line 1 is the title with priority and mark glyphs. Line 2
  leads with the assignee: `● forge` while the agent works on the card,
  `○ forge` when idle, `you` in the accent colour, `unassigned` muted.
  After it: test state (`✓`/`✗`), PR number, comment count, then the slug.
- **Card page.** `enter` opens the selected card as a page in the board
  area, in sections: title, status/priority/labels, ASSIGNEE (with live
  working state and hand-off history), WORK (worktree and branch, PR,
  tests, runs and spend), the agent's plan and verify verdict, COMMENTS
  (all of them, markdown-formatted, author and age) and ACTIVITY.
  `j`/`k` scroll; `N` comment, `a` assign, `T` take, `o` open thread,
  `r` runs, `esc` back.
- **Assign picker.** `a` opens a fuzzy picker: `you`, every agent (role
  and whether it is busy), `unassigned`. Typing filters.
- **Reassign to an agent.** The hand-off brief names the previous owner
  and the card's worktree and branch, so the new agent continues there;
  the card-bound turn already resolves that worktree. If the previous
  agent is mid-turn on the card's thread, that turn is stopped first.
- **Take it.** `T` assigns the card to you, creates its worktree on the
  conventional branch when it has none (asks for the member only when
  the workspace has several), moves it to active, and opens the editor
  with the worktree as a tree root. Commit, push and PR keys already
  exist (`c`, `u`, `p`).
- **Agents** keep using `task_assign` / `task_comment`; their comments
  render with the same formatting.

## Acceptance
- [x] Card goldens: working agent, idle agent, you, unassigned, narrow.
- [x] Card page shows every comment formatted, scrolls, and keys work.
- [x] Picker filters and assigns; reassigning an agent's card stops its
      turn and hands off with the worktree in the brief.
- [x] Take: assign + worktree + active + editor (fake seams in tests).
- [x] Background sweep covers the card page and the picker.

## Notes
- An agent's `task_assign` to another agent is handed off by the runtime
  (`Runtime.agentReassigned`, from `WatchTasks`); the human's reassignments
  are handed off by the board (`handoffFrom`). A brief never goes into a
  previous owner's DM: the card is rebound to the new owner's DM.
- `Runtime.CancelThread` stops in-flight turns on a thread (context cancel).
- The picker accepts a typed name off the roster (enter with no match).
- Editor: `OpenWorktree(label, dir)` adds the worktree as a tree root.
