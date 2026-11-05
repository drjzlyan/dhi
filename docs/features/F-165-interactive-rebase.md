# F-165 — Interactive rebase in a terminal tab, edited in DHI's buffers

Status: done · gap G-G6

`git rebase -i` needs an editor for the todo list and for commit messages. Left to the user's
`$EDITOR` it opens a nested vim inside the drawer tab; reordering commits should happen in the
buffer the user already knows.

## Decision
- IPC: nothing existing fits (the loopback MCP endpoint is for agents), so a unix socket in a
  private `os.MkdirTemp` folder (owner-only, removed when the rebase ends or DHI quits). The temp
  dir, not `.dhi/tmp`, because a socket path is limited to about 100 bytes.
- The shim is the DHI binary itself, reached through a symlink named `dhi-edit` in that folder.
  `main` checks the name it was started under (`editshim.IsShim`) before anything else. It is
  not a subcommand: `dhi help` and the unknown-command path are unchanged (ADR-0004).
- `GIT_SEQUENCE_EDITOR` and `GIT_EDITOR` both name the link; they are added only to the rebase
  tab's environment, never to the user's shell.
- Closing a buffer a program waits on is the answer: `:wq`/`:q`/`:q!` exit 0 (git uses the file as
  it is on disk, as with vim); `:cq` exits 1 and git aborts the step. `:cq` is new in textbuf.

## Behaviour
- `:rebase [<base>]` runs `git rebase -i <base>` in a new drawer tab in the member repo (the active
  buffer's member, else the git panel's). Without a base: the upstream; else the merge-base with
  the default branch (`origin/HEAD`, else the only one of `main`/`master`/`trunk`). With no default,
  several defaults, or nothing beyond it, the status line says so and asks for `:rebase <base>`.
- Git's todo (`git-rebase-todo`) and commit messages (`COMMIT_EDITMSG`) open as buffers with focus;
  the todo is coloured (command, hash, `#` comments; `pick reword edit squash fixup drop exec`...).
  They are never autosaved, swapped or put in local history.
- A stopped rebase (conflict, `edit`) raises a "Rebase stopped" toast naming the next step. The
  conflicted file opens with the F-156 tools (`:conflict ours|theirs|both`, `]x`, stage offer).
- `:rebase continue|abort|skip` run `git rebase --continue|--abort|--skip` in a tab with the same
  editors (a reword or a resolved commit opens its message in a buffer). Without a rebase in
  progress it says so. `:rebase <base>` during one refuses.
- When a rebase tab's git exits and no rebase remains, the shim and its folder are removed.

## Acceptance
- Shim: `TestShimBlocksUntilDoneAndExitsZero`, `TestShimExitsNonZeroOnAbort`,
  `TestShimFailsWhenServerCloses`, `TestShimWithoutDHIRefuses`, `TestEnvQuotesPath` (editshim).
- Buffer: `TestCQQuitsWithFailure` (textbuf); `TestLexRebaseTodo` (syntax).
- Real git in a temp repo: `TestRebaseReorderThroughTheBuffer` (`ddp`, `:wq` reorders history),
  `TestRebaseCQAbortsTheStep`, `TestRebaseConflictThenContinue` (conflict, `:conflict theirs`, stage
  offer, `:rebase continue`, message buffer), `TestRebaseAbort`, `TestRebaseQuitWithoutSavingKeepsTheTodo`,
  `TestRebaseBaseChoice`, `TestRebaseWithoutOneInProgress`.
- Every way out of DHI (nothing unsaved, save all, keep for next time, quit without saving) goes
  through one shutdown hook (`surfaces.Shutdowner`) that removes the shim (`TestEveryQuitPathShutsSurfacesDown`,
  `TestCancelledQuitDoesNotShutDown`; editor `TestShutdownClosesRebaseServer`).
