# F-021: Workspace dashboard — INBOX · BOARD · CHANNELS · REPOS

Status: planned (M11 P3/P4) · Milestone: M11 · Depends on: F-003,
F-016 (inbox), F-017 (unread), F-023 (settings owns management).
Companion to ADR-0014.

## Summary

The Workspace surface drops from nine sections to four —
INBOX · BOARD · CHANNELS · REPOS — and becomes the JIRA-like
dashboard: a kanban board of task cards with a detail pane, a
notifications section, the Slack-like team floor, and repo member
management. Management UI (teams, packs, standards, autopilots,
agents) moves to Settings (F-023).

## Sections

### BOARD (default landing)

- Four fixed columns in flow order: `backlog · active · in-review ·
  done` (tasks.Statuses), one card line per task: status-colored
  slug, title (elided to the column width), assignee/team chip.
- `h/l` (or ←/→) move the column cursor; `j/k` move the card cursor
  within the column; `g/G` jump to first/last card of the column.
  Empty columns show a dim `—` row.
- The selected task renders in a **detail pane** (right on wide
  terminals, below on narrow): title, status, assignee, team, bound
  thread, changesets (member/branch/path), runs rollup (`N runs ·
  cost/tokens` per F-014), PR number/url, created/updated.
- Keys (preserved from TASKS): `n` new, `s` cycle status, `a` assign,
  `w` attach worktree, `t` bind thread, `x` remove (confirm), `p`
  create PR, `c` commit, `u` push, `r` run replay. New: `o` open the
  bound thread (or the assignee DM) in CHANNELS — the jump from board
  to floor. Forms ride the shared kit.Form (F-024).
- Store: reads/writes go through `internal/tasks` only (List/Get/
  Create/SetStatus/Assign/BindThread/SetPR/RecordChangeSet/Commit/
  PushBranch/Remove); `Subscribe` pings re-render — no caching, no
  new store code.

### INBOX

- Unchanged behavior (F-016/F-017): pure aggregation, jump seams,
  snooze. Section index moves from 8 to 0; `enter`/`o` jump targets
  stay (editor chat approvals, CHANNELS thread via the new layout,
  run replay, Reviewer).

### CHANNELS

- Rebuilt as the Slack-like floor — spec'd in F-022.

### REPOS

- The old MEMBERS pane renamed: member repo list, `a` add
  (path/git-URL), `r` rename, `d` remove-with-confirm. Working trees
  are never deleted (unchanged). Behavior identical; label and copy
  update.

## Layout contract

- Docked: workspace rail (26 cols, four labels + counts) | main pane.
- BOARD and CHANNELS sub-divide the main pane (columns / three
  sub-columns). Narrow terminals (< dockMinWidth) keep the compact
  centered stack; BOARD degrades to single-column focus (←/→ selects
  the column, cards list inside it).
- Section counts in the rail: INBOX = attention count, BOARD = open
  (non-done) task count, CHANNELS = total unread, REPOS = member
  count.

## Acceptance criteria

- Section enum is `secInbox, secBoard, secChannels, secRepos`; `[`/`]`
  wraps over 4; default section after boot is BOARD; the boot hero
  (not-in-workspace) is unchanged.
- BOARD: four columns render every task exactly once, in status
  order; status cycle/assign/attach/bind/remove/PR/commit/push/replay
  behave as today (same store calls); `o` on a task with a bound
  thread lands CHANNELS on that thread; `o` without a thread but with
  an assignee lands the assignee DM; neither → named flash, no jump.
- INBOX jumps still resolve (approval → editor chat, mention →
  CHANNELS thread, run_failed → replay, in_review → Reviewer).
- REPOS add/rename/remove round-trips through workspace.Store with
  the same persistence-before-visibility rule.
- Goldens: board (populated), board narrow (single-column), repos.
- `make verify` green.
