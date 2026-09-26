# ADR-0020: Feature workflows — layered steps enforced at DHI's seams

Date: 2026-09-26 · Status: accepted · Companion to: ADR-0019 (DHI owns
the loop), ADR-0011 (no silent fallbacks), F-011 (standards layers),
ADR-0009 (hermetic git / worktrees). Serves: F-031.

## Context

Standards (F-011) are *guidance*: prose injected into the system block
and followed on trust. The user's crew needs more than guidance for a
feature: work must happen in a worktree, on a branch, tested before it
is proposed. Those steps are not prose — several of them are machinery
DHI already owns (worktree attach, commit, push, PR, review), while
others (write the test first) are genuinely unverifiable. A workflow
must therefore be **half declared procedure, half enforced gate**,
without pretending to verify what it cannot.

## Decision

1. **A workflow is an ordered list of steps, each bound to a seam or a
   tool.** A step declares `id`, `title`, `guidance` (prompt text),
   `gate` (`warn | block | exception-approval`), and the seam/tool it
   binds (e.g. `worktree`, `run:test`, `git:commit`, `review`). A step
   with no bindable seam can only be `warn`.
2. **Workflows layer like standards**: builtin → workspace → team →
   agent, one active workflow per feature task, resolved fresh per turn,
   pure and table-tested. A named malformed workflow refuses the turn
   (ADR-0011), exactly as malformed standards do.
3. **The builtin `feature` workflow** is:
   `worktree_create → implement → test → commit → push → open_pr →
   review`, with `worktree_create` and `test-before-pr` as **hard
   blocks**, `review` as **exception-approval**, and everything else
   guidance. Committing without a worktree, or opening a PR while the
   workflow's test command fails, is refused at the seam by name.
4. **TDD is guidance, not a gate.** "Tests were written first" is not
   reliably observable; blocking on it would manufacture a false
   guarantee. The step advises it; the `test` step enforces only that
   the declared command passes.
5. **A bypass is a named, recorded act.** `exception-approval` steps
   require an explicit human decision routed through the approvals
   queue; the run transcript records the bypass and the reason. Silence
   is never acceptance.
6. **Execution is DHI's, expression is the agent's.** The workflow
   supplies both the prompt guidance seen by the engine and the gate
   checked by DHI; the engine still does the work (ADR-0019). A step
   may add allowlisted `run` commands to the task's tool scope for its
   duration only.

## Consequences

- `internal/agentkit/workflow` is a new store; `pack.toml` may ship
  workflows (ADR-0022). Tasks carry the active workflow id.
- The commit/push/PR seams (tasks, reviewer) consult the active
  workflow before acting; refusals name the unmet step and the fix.
- Doctor reports a `workflows` row: malformed definitions, dangling
  step seams, and tasks whose active workflow is missing.
- Workflows make the "how do we work on a feature" convention
  first-class and shareable, replacing wiki prose and ad-hoc prompts.
