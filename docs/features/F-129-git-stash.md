# F-129 — Git stash from the panel

Status: done · gap G-G4

The git panel could stage, commit, switch branches, push and pull, but setting work aside meant
leaving for a terminal. Amends F-121.

## Behaviour
- `z` in the git panel (either tab) opens a stash list, newest first (ref, message, age).
- `n` asks for an optional message, then runs `git stash push [-m msg]`. Tracked changes only,
  as git does by default. A clean tree reports `no local changes to stash` instead of nothing.
- `a` (or enter) applies the selected stash and keeps it. `p` pops it. The list closes and the
  buffers, tree, gutters and panel refresh. On a conflict git keeps the stash; the error shows
  in the list and the conflicted files show in the status.
- `d` asks first: `y` drops the stash, any other key cancels.
- Everything runs the user's own git (ADR-0044) through `gitcore.Runner`:
  `gitcore.Stashes`, `StashPush`, `StashApply`, `StashPop`, `StashDrop`. Refs must look like
  `stash@{n}`.

## Acceptance
- Stash, list, apply (keeps), pop (drops), drop, a non-stash ref refused, a clean tree refused
  (`gitcore.TestStashLifecycle`).
- From the panel: `z`, `n` with a message stashes and cleans the tree; `d` then a non-`y` key
  keeps it; `a` restores and keeps; `p` restores and drops; `d`, `y` drops; a clean tree shows
  the refusal (`editor.TestStashFromThePanel`).

## Not in this phase
Untracked files (`-u`), stash branch, showing a stash's diff.
