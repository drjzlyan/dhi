# ADR-0032: The goal loop, DHI-provisioned worktrees, and the autonomy envelope

Date: 2026-10-13 · Status: accepted · Companion to: ADR-0019 (DHI owns
the loop), ADR-0020 (feature workflows), ADR-0011 (no silent fallbacks),
ADR-0031 (episodes, resume, budgets). Serves: F-067 (M29 P3).

## Context

For "assign a card, walk away, get a reviewable PR", DHI needs a loop
that works without a person watching. Several gaps blocked that:
- **Success was self-reported.** One spawn ran per turn, and success was
  whatever the agent said.
- **Every in-task mutation through DHI's tools asked a human.** One
  unanswered approval stalled the work.
- **The P2 live eval failed 0/8 on DHI.** Following the feature workflow,
  agents created their *own* worktrees as sibling directories. The work
  was real but invisible to DHI: not verified, not on the card, not
  PR-able.
- **DHI's file tools wrote to the shared checkout** while the CLI's
  native tools wrote to the cwd.
- **The board's hand-off never bound a card to its brief**, so assigned
  work ran as a plain chat turn.

## Decision

1. **DHI provisions the task worktree.** It does this on a card-bound turn
   whose card has no worktree, when the workspace has exactly one member.
   The worktree goes through the existing attach seam, on the
   conventions' task branch, before the first spawn.
   - **Autopilot run cards** (`auto-…`) are chores and are never
     provisioned.
   - **Ambiguous cases** (several members) and failures post a named
     notice.
   - **When no worktree exists**, the workflow text forbids the agent from
     making a private one.
   - **On every turn with a worktree**, the agent is told its path, that
     it *is* member X's checkout, and the verify command.
   - **One tree, not two.** DHI's file tools resolve that member's vpaths
     into the worktree, so the agent works on one tree.
2. **DHI verifies, the agent does not grade itself.** After a successful
   spawn in a task worktree that *changed*, DHI runs the project's check
   (`go test ./...` for Go modules) through the same sandboxed runner as
   the `run` tool. "Changed" means HEAD moved or the tree is dirty,
   detected via hermetic git; without that seam it assumes changed.
   - **Pass:** set `TestsPass`, then reply.
   - **Fail:** continue the CLI session with the failure tail, up to
     `agents.max_iterations` rounds (default 3).
   - **The same failure twice:** "stuck" → an `@you` reply, which lands in
     the Inbox.
   - **The cap is reached:** the last output goes to `@you`.
   - **An environmental failure** (network, module download, sandbox
     denial) is named and never sent back as a bug.
   - **Turns that changed nothing** are not verified. Status questions on
     a card never trip a test run.
3. **The autonomy envelope** (`agents.autonomy = "envelope"`, the
   default).
   - **Where it applies:** a task turn working in its card's worktree.
   - **What it covers:** for DHI's `run`, `git_commit`, and `write`/`patch`
     whose resolved target is inside the worktree, plus `task_comment` and
     `task_status` on the bound card, an Ask becomes Auto.
   - **What it never covers:** Deny is never lifted. Push, PR, network,
     admin, settings, new tools and chat turns are never covered.
   - **The trust argument:** the human approved the task, the work is
     confined to a disposable branch, and the PR is the review gate.
   - **Opting out:** `"ask"` restores per-mutation approval.
4. **Grants persist.** "Allow always" is stored in `.dhi/grants.json` and
   loaded at boot. A malformed file is named and ignored, never trusted.
   Settings → AGENTS lists the grants and revokes them.
5. **Watchdog.** A spawn silent for `agents.stall_seconds` (default 900)
   is killed as `stall`. That failure is retryable, and the session
   continues.
6. **The hand-off binds.** Posting a card's brief binds the card to that
   message's thread.

## Consequences

- Upgrading users get the envelope by default. It only ever relaxes Ask
  inside a task worktree, it is a single CONFIG row, and doctor and
  Settings show it.
- The host CLI's native tools were already ungated inside the OS sandbox
  (ADR-0024 containment). The envelope aligns DHI's own tools with that
  reality inside the task branch. It does not widen the sandbox.
- `TestsPass` now reflects DHI's own check, true or false, not only an
  agent-run `go test` through `run`.
- Only Go has a verify command today. Other ecosystems get a named
  "unverified" notice until their runners are allowlisted.
