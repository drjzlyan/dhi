# F-086 — Agents can run commands and commit on their own branch

Status: done (M32 P1) · ADR-0039

## Why
anvil was asked in a DM to analyse and fix a repo. It fixed four bugs but
ran nothing and committed nothing. Two separate causes:
1. Every Bash call failed with `EPERM … mkdir '/private/tmp/claude-501/<cwd>'`.
   DHI's macOS sandbox lets agents write only inside the workspace and the
   CLI's own state folders. Claude Code makes its per-user temp folder under
   `/tmp` before it runs any command.
2. A DM turn has no task card, so it runs in the shared checkout on `main`.
   The only thing that stopped a commit to `main` was a prompt line.

## Behaviour
- **Private temp dir.** Every agent CLI spawn gets
  `TMPDIR=<ws>/.dhi/tmp/<agent>` (mode 0700), and so does each variable the
  adapter declares in `CLI.TempEnv`. For claude that is
  `CLAUDE_CODE_TMPDIR`. The run/verify runner uses `.dhi/tmp/run`.
  `.dhi/tmp` stays writable under the `.dhi/` guard, and it is gitignored
  (`setup.RuntimeEntries`, so existing workspaces get it through the
  gitignore repair).
- **Live check (claude 2.1.285, macOS).** The test ran under a Seatbelt
  profile that denies writes to `/private/tmp` and `/private/var/folders`.
  With only `TMPDIR` set, Bash still failed with the screenshot's exact
  error. With `CLAUDE_CODE_TMPDIR` set, it ran. No `/tmp` root is added to
  the sandbox.
- **`work_branch {slug, title, member?, carry?}`** is a harness tool, in
  the git scope, so it goes through approvals.
  - Creates the card, assigned to the agent, or resumes a card the agent
    already owns.
  - Binds the card to this thread and provisions the convention branch and
    worktree through the same `Provision` path task turns use.
  - `carry` (on by default) moves only what changed during this turn:
    files already dirty when the turn began (yours, or another agent's)
    stay in the shared checkout, and the agent is told which. `.dhi/` is
    never carried. `work_branch` needs no approval (M32 P7).
  - It then asks the turn to continue. When the spawn ends, DHI posts
    `@agent continue: …` in the thread, and the new turn runs in the
    worktree.
  - A thread that already has a worktree gets told where it is. Another
    agent's card is refused.
- **`git_commit` refuses the default branch** (main, master, or the
  remote's HEAD) by name, before spending an approval, and points at
  `work_branch`.
- The prompt grounding tells every agent to call `work_branch` before
  changing code outside a task worktree. The no-worktree workflow note
  points at it too.
- The runtime tracks the turns it dispatches (`WaitBackground`). This
  fixed a test that failed on the base commit because a hand-off turn
  outlived its temp dir.

## Acceptance
- [x] spawn env: TMPDIR and CLAUDE_CODE_TMPDIR → private 0700 dir, inherited TMPDIR replaced
- [x] git_commit on master refused, no approval queued; feature-branch commits unchanged
- [x] work_branch: card + bound thread + worktree, relaunch requested, next workdir = worktree
- [x] work_branch: another agent's slug refused; second call reports the existing worktree
- [x] continuation post wakes the agent in the same thread
- [x] carry leaves files dirty before the turn in the shared checkout (real git)
- [ ] live: a DM "analyse and fix" runs Bash, calls work_branch, commits on task/<slug> (M32 P7)
