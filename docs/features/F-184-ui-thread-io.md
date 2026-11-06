# F-184 — Slow work never runs on the UI thread

Status: done · wizard, settings, reviewer, workspace board, editor, ideator

## Problem
Setup froze for 4–6 s on "Pick a team": `teamStep.Enter` ran `DetectAll` (every coding CLI's
`--version`) on the UI goroutine, reached from the palette through `Model.Init`. A watchdog on
`App.Update`/`View` over the README tapes, and a scan of every `context.Background()` call in
`internal/tui` and of the hooks `cmd/dhi` injects, found more of the same kind. Each blocks for as
long as git, the network, a language server or a child process takes:

| Where | What blocked | Measured / bound |
|---|---|---|
| wizard team and CLI steps | `DetectAll` on entry, on `r`, after an install | 4.4 s live |
| Settings identity row | `git config` inside `View` | 110 ms live |
| Settings identity form | `SuggestIdentity` (repos, GitHub, account) | up to 6 s |
| Settings skill install | `skillprop.Install` fetch | up to 2 min |
| Reviewer submit | `PlanReview` reads the diff (git or gh) | up to 5 min |
| Board take/attach | `git worktree add` (full checkout) | 0.4 s on this repo |
| Editor file tree | go-git status per member, on every render after a save | 0.1 s on this repo |
| Editor git panel | status + log + conflict list on every refresh | 0.23 s on this repo |
| Editor stage / unstage / commit | go-git status inside each | 0.1 s+ on this repo |
| Editor stash | push / apply / pop / drop / list | git, scales with the tree |
| Editor undo-turn | snapshot diff, then restore | 20 s bound |
| Editor `:task` | `just --list` (evaluates the justfile) | 5 s bound per member |
| Editor `:sym`, `:calls` | LSP request while the server indexes | 3 s bound |

"This repo" is DHI itself (1,400 files): a large repo is 10–100× slower.

Found on the way: the Ideator dispatched turns with a context it cancelled when `requestTurn`
returned, so every turn it started was cancelled at once (`Handle` runs turns in goroutines).

## Behaviour
- Every row above runs in a goroutine or `tea.Cmd` and reports back with a message, in each
  surface's own style: the wizard's `Enter` command plus step `Update`, the settings `events`
  channel, the reviewer and workspace `send`, the editor's `termMsgs` and `m.queue`.
- While the work runs the surface says so: "checking which coding CLIs are installed…",
  "checking…", "looking for a suggestion…", a busy form, "creating the worktree…",
  "reading git status…", "committing…", "stashing…", "restoring…", "looking for tasks…",
  "asking the language server…". The old rows stay visible during a refresh.
- Nothing saves on stale or missing data: the team and CLI steps ignore enter until detection
  lands (esc still skips). A second submit, install, commit, stage or stash waits for the first.
- Index writes (stage, unstage, commit) run one at a time, so they apply in the order the keys
  asked. Background reads open their own go-git handle and never share one with a write.
- Generation counters drop results that a newer request or an invalidation overtook (wizard
  steps, tree status, panel reads). A reply for a dialog that was closed is dropped.
- Board attach now always seeds cross-project propagations (F-032). Before, the AI-named-branch
  path skipped them.
- The Ideator passes `context.Background()`; the runtime bounds and cancels turns itself.

Kept synchronous, because each is bounded and fast or ordering depends on it:
- the graph and file log (about 30 ms, capped at a commit limit);
- hunk and line staging through `git apply` (about 20 ms, applied in order);
- `git config` writes (identity apply) and single-file `git add` when resolving a conflict;
- `Runtime.Handle` and `bus.Post` (they dispatch and append);
- `term.Start` (it only spawns);
- format-on-save (a bounded wait that save ordering depends on);
- `:task` without a justfile to run (file reads only);
- conflict checks on save and on a rebase stop (rare, about 100 ms).

## Acceptance
- [x] Team step: no detection before the command runs, a checking line, enter waits (`TestTeamStepDetectsCLIsOffTheUIThread`)
- [x] CLI step: entry, `r` and post-install refresh are commands; a stale reply is dropped (`TestCLIStepDetectsOffTheUIThread`)
- [x] Settings identity row renders "checking…" while git blocks, one read only (`TestIdentityRowNeverBlocksTheRender`); suggestion fills the open form (`TestIdentityRowSuggestsConfirmsAndSets`)
- [x] Skill install runs with the form busy (`TestSuggestedSkillReviewAndInstall`)
- [x] Reviewer plan is async; a late plan after esc is dropped (`TestSubmitPlanRunsOffTheUILoop`)
- [x] Board attach returns at once and opens the worktree when done (`TestTakeCardAssignsProvisionsAndOpensEditor`)
- [x] Tree status loads in the background; an overtaken read is dropped (`TestTreeGitStatusLoadsOffTheUIGoroutine`)
- [x] Panel reads in the background; an overtaken read is dropped (`TestGitPanelReadsOffTheUIGoroutine`)
- [x] Commit is async and one at a time (`TestGitPanelCommitsOffTheUIGoroutine`); stage waits its turn and blocks a commit beside it (`TestGitPanelStagesOffTheUIGoroutine`)
- [x] Stash push runs in the background with keys waiting (`TestStashFromThePanel`)
- [x] Undo-turn diff and restore are async (`TestUndoAgentTurn`)
- [x] `:task` through `just` returns at once; a second waits (`TestTaskListThroughJustRunsOffTheUIGoroutine`)
- [x] `:sym` does not wait for the server (`TestSymbolPickerFilterAndJump`); `:calls` prepare is queued (`TestCallsIncomingTreeExpandsAndJumps`)
- [x] Ideator turn context outlives dispatch (`TestRejectRoutesToAuthor`)
- [x] Watchdog run of the four README tapes on the fixed tree: no Update or View over 50 ms
