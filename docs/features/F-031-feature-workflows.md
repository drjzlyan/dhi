# F-031: Feature workflows

Status: planned (M16) · Companion to: ADR-0020 (feature workflows),
ADR-0019 (DHI owns the loop), F-011 (standards layers), F-021 (board).
Product rule: how the crew works is configured in the IDE, shared, and
followed by every agent.

## Summary

Standards tell an agent *what good code is*; a workflow tells it *how to
do a feature here* — worktree, implement test-first, test, commit, push,
open a PR, get review. Workflows layer like standards and are enforced
where DHI owns the seam, so "create a worktree and run the tests" is not
a suggestion the agent may skip.

## Part A — definition

- `.dhi/workflows/<slug>.toml`, strict decode. A workflow is an ordered
  list of steps: `id`, `title`, `guidance`, `gate`
  (`warn | block | exception-approval`), and a `bind` to a seam
  (`worktree`, `run:<cmd>`, `git:commit`, `git:push`, `pr`, `review`)
  or `none` (guidance-only).
- Layering: builtin → workspace → team → agent, one active workflow per
  feature task, resolved fresh per turn (pure, table-tested). Malformed
  workflow refuses the turn by name.
- A step may add allowlisted `run` commands to the task's scope for its
  duration only.

## Part B — the builtin `feature` workflow

`worktree_create → implement → test → commit → push → open_pr → review`

- **hard blocks:** worktree-before-commit; tests-pass-before-PR.
- **exception-approval:** review-required-before-merge.
- **guidance:** implement (TDD advice); test is the declared command.
- A bypass is a named, recorded human decision; never silent.

## Part C — surface

- Settings WORKFLOWS section: browse (builtin/local), author/edit via
  strict forms, attach a default to a team or agent.
- Board: a task shows its active workflow and current step; a blocked
  action names the unmet step and the fix.
- Packs may ship workflows (ADR-0022).

## Acceptance criteria

- [x] The builtin `feature` workflow refuses a commit with no worktree
      and a PR with failing tests, naming the step and the fix. (P1/P2:
      `dhitools` git_commit calls `Deps.Gate("git:commit")`;
      `toolbridge` prOpen calls `Bridge.Gate(slug,"pr")`; progress is
      durable task state — `Worktree` from the card's changesets,
      `TestsPass` set when the served `run` sees a passing `go test`.)
- [x] An `exception-approval` step routes through the approvals queue and
      records the bypass + reason in the run. (P3: completing a task
      crosses the `review` exception — the bypass routes through
      `Bridge.approve` and is recorded as a `tasks.Bypass{Step,Reason,At}`
      on the card, which also clears the step thereafter. A declined
      bypass refuses and never completes the task.)
- [x] Workflows layer builtin→workspace→team→agent; a malformed
      definition refuses by name. (P0/P1: `workflow.Resolve` precedence
      agent manifest → org teams → `.dhi/workflows.toml` default →
      builtin; malformed refuses at `Turn` and at `activeWorkflow`.)
- [x] Settings authors a workflow through a strict round-trip form. (P3:
      WORKFLOWS section browses builtin/local, authors a new definition
      via `n` (validated by `workflow.Save` before it lands), sets/clears
      the workspace default via `d`, and previews steps via `v`.)
- [x] `doctor` warns on malformed workflows, dangling seams, and tasks
      whose active workflow is missing. (P2: `Workflows(wsRoot)` row.)
- [x] `make verify` green per phase.

## Deferred

- Workflow branching (conditional steps) and parallel steps.
- Executable skill scripts (a skill that carries steps).
