# F-067 — Goal loop, provisioned worktrees, autonomy envelope, grants, watchdog

Status: done (M29 P3) · ADR-0032

## Problem
For task → PR unattended work, DHI must own the outer loop. Before this
feature:
- One spawn ran per turn. Success was self-reported.
- `TestsPass` flipped only when the agent ran `go test` through DHI's
  `run` tool.
- Every in-task mutation through DHI's tools asked, so one unanswered
  approval stalled the work.
- "Grant always" vanished on restart.
- A silent CLI held the turn indefinitely.
- The P2 live eval (DHI 0/8) exposed two more problems:
  - Agents built *private* worktrees that DHI never learned about.
  - DHI's own file tools wrote to the shared checkout.
- The board's hand-off never bound a card to its brief, so assigned work
  ran as a chat turn.

## Behaviour
- **Provisioned worktree.** A card-bound turn whose card has no worktree,
  in a one-member workspace, gets one first, on the conventions' task
  branch (`Config.Provision` → `tasks.Store.Attach`).
  - Autopilot run cards (`auto-…`) are never provisioned.
  - Several members, or a failure, gives a named notice.
  - Without a worktree, the workflow text forbids a private one.
  - With one, the task block says it *is* member X's checkout and names
    the verify command, and the workflow marks "Work in a worktree" as
    done.
- **One tree.** DHI's file tools resolve the card's members into their
  worktrees (`Deps.MemberRoots`).
- **Verify loop.** After a successful spawn in a task worktree that
  changed (HEAD moved or dirty, via hermetic git; without that seam it
  assumes changed), DHI runs `go test ./...` with the sandboxed runner.
  - **Pass:** `TestsPass` is set, a "verified" notice is posted, then the
    reply.
  - **Fail:** the session continues with the failure tail, up to
    `agents.max_iterations` (default 3).
  - **The same failure signature twice:** stuck, then an `@you` reply
    (Inbox).
  - **Cap reached:** an `@you` with the last output.
  - **An environmental failure** (network, module download, sandbox) is
    named and not looped.
  - **No change:** no verify.
- **Autonomy envelope** (`agents.autonomy`, default `envelope`).
  - **Covered:** in the card's own worktree, Ask becomes Auto for `run`,
    `git_commit`, and `write`/`patch` whose target is inside the worktree,
    plus `task_comment` and `task_status` on the bound card.
  - **Never covered:** Deny stays deny. Push, PR, network, admin and chat
    turns are never covered.
- **Persisted grants.** `.dhi/grants.json` is loaded at boot; a malformed
  file is named and not trusted. Settings → AGENTS lists them under
  ALWAYS ALLOWED, and `r` revokes the selected agent's (the hint shows
  only when there is something to revoke).
- **Watchdog.** `agents.stall_seconds` (default 900; 0 = off) kills a
  silent spawn as `stall`, which is retryable and continues the session.
- **`plan_update`.** A harness tool served to every agent, no allowlist
  needed. It stores the plan on the episode. The board card pane shows
  the plan, DHI's verify verdict and the episode spend (turns, cost,
  cache share, session kept).
- **Hand-off binding.** Posting a card's brief binds the card to that
  thread.
- **Settings.** CONFIG → agents & budgets gains `autonomy`,
  `max_iterations` and `stall_seconds`.

## Acceptance
- [x] Verify pass: one round, `TestsPass` true, the reply is posted.
- [x] Fail → fix: two rounds, the second resumes with the failure tail.
- [x] Repeated failure (timings differ) → stuck and `@you`, stopping
      after two rounds.
- [x] The iteration cap is honoured, and the hand-back goes to `@you`.
- [x] A chat turn never verifies. A turn with no worktree never verifies.
      An unchanged worktree is not verified (real git).
- [x] An environmental failure is named, not looped.
- [x] Provisioning: notice, worktree cwd, the member mapping and verify
      lines, and the workflow step marked done. An ambiguous case is
      named. Autopilot cards are skipped. With no worktree, private ones
      are forbidden.
- [x] Envelope table tests, including escape and prefix-sibling paths.
      Through the handler, DHI's `write` lands in the worktree with no
      ask.
- [x] Grants round trip and revoke. A malformed file refuses. Settings
      lists and revokes them.
- [x] The watchdog kills a silent stub as `stall`.
- [x] `plan_update` is served without an allowlist entry and validated
      strictly. The board shows the plan (golden), the verify verdict and
      spend.
- [x] Hand-off binds the card to its brief.
- [x] Live eval: edits land in `.dhi/worktrees/…`, the verify loop fires,
      in-worktree asks are 0. Recorded below.
- [x] `make verify` and lint are green.

## Live eval (2026-10-13)
Run: claude, `DHI_EVAL_REPEAT=2`, four fixtures. Arm order alternated.

| arm | success | tokens/ok | cached | cost (8 runs) | mean wall |
|---|---|---|---|---|---|
| raw | 8/8 | 128.5k | 79% | $2.56 | 22.3s |
| dhi | 8/8 | 157.9k | 86% | $2.27 | 25.9s |

- **Success.** DHI went from 0/8 (P2) to 8/8, with every result on a
  DHI-provisioned task branch and committed. The kept `fix-failing` runs
  each show the same four things:
  - "created worktree .dhi/worktrees/eval-fix-failing/app";
  - an edit inside the worktree;
  - a commit "Fix Split dropping the remainder cents" on
    `task/eval-fix-failing`;
  - "verified: go test ./... passed (round 1)".

  The shared checkout stayed clean. The raw arm edits the checkout and
  commits nothing.
- **Cost.** DHI is 11.6% cheaper: it writes 22% less cache and serves 86%
  of its prompt from cache.
- **Speed.** The wall-time gap closed from 1.9× (P1) to 1.16×.
- **Asks.** 0 asks in both arms. No served-tool mutation needed an
  approval.
- **What it doesn't show.** These fixtures pass on the first verify round,
  so the fix loop was exercised only by the runtime tests. A harder
  fixture that needs a second round is a P4 follow-up.
