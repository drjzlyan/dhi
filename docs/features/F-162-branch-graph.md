# F-162 — Branch graph and cherry-pick

Status: done · gap G-G7

Seeing how branches diverge, or moving one commit between them, meant leaving for a terminal.

## Decision
- The graph is the user's `git log --graph --all --decorate=full --date=relative` with a unit-separator
  format (`gitcore.GraphLog`); `gitcore.ParseGraph` is pure. Lane art is kept verbatim and coloured per lane
  from the theme (`theme.GraphLane`); connector lines are rows without a hash and are never selectable.
- Capped at 500 commits (`graphLimit`). Cherry-pick, abort and branch-at are plain git calls (ADR-0044).
- Merge commits (no single change) and the root commit are refused before the confirm.

## Behaviour
- `:graph`, or `g` in the git panel (free; the panel uses s S u c C d b z h p P), opens the graph of the
  member repo: lanes, `HEAD`, branches (current one bold), remotes, `tag:` names, then subject, with
  author and relative date dimmed when they fit.
- `j`/`k`/arrows move over commits; `pgup`/`pgdn` jump a page. `enter` opens the commit's whole diff in the
  read-only F-140 viewer (each hunk names its file); `esc` returns to the graph, `esc` again closes it.
- `c` asks "cherry-pick <sha> onto the current branch? y"; any other key cancels. On success the tree
  refreshes and the graph reloads. On a conflict the graph stays open and names the files and the way
  on: `]x` / `:conflict` (F-156), `git commit` to finish, `:cherrypick abort` to give up. A pick that
  turns out empty says so. Other failures (dirty tree) show git's message.
- `:cherrypick abort` runs `git cherry-pick --abort` (says so when none is in progress); bare
  `:cherrypick` prints usage.
- `b` prompts for a name and creates the branch at the commit without switching; a taken or invalid
  name reports and keeps the prompt.
- Nothing mutating runs without the `c` confirm or the typed `b` name.

## Acceptance
- Lanes, decorations and connector rows parse (`TestParseGraph`); real two-branch repo, clean and
  conflicting picks, abort, branch-at (`TestGraphCherryPickAndBranch`).
- Graph shows all branches, diff opens read-only, esc steps back, `g` opens it (`TestGraphOpensAndShowsACommit`).
- Selection only lands on commits (`TestGraphMovesOverCommitsOnly`).
- Confirm gate, clean pick, conflict guidance, abort (`TestGraphCherryPick`); merge/root refused
  (`TestGraphRefusesMergesAndRoot`); branch at commit (`TestGraphBranchHere`).
- Whole-commit patch splitting (`TestSplitCommitPatch`).
