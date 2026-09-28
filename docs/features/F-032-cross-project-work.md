# F-032: Cross-project work & dependency graph

Status: planned (M17) · Companion to: ADR-0021 (cross-project
dependencies), ADR-0009 (worktrees), F-021 (board). Product rule: a
change that crosses projects is one piece of work, not N disconnected
ones.

## Summary

A workspace holds many member repos, and a real feature often spans
them. Today a task can name multiple members but only the **first** is
used — a correctness hole. F-032 makes multi-member work real, adds a
declared dependency graph, and turns "project B depends on A" into a
visible proposal when A changes.

## Part A — all changesets

- A task may attach one worktree per member (branch per member). Agent
  cwd, commit, push, and PR operate across **all** changesets; per-member
  failure is named, never silently skipped.
- The board's fact pane lists every member@branch; the run record covers
  the whole set.

## Part B — declared dependencies

- `workspace.toml` gains `[[dependency]] from to kind`
  (`module | api | build`); strict decode, dangling member = named
  doctor warning.
- A workspace dependency view renders the graph.

## Part C — propagation as proposal

- When a task changes member A and a declared edge A→B exists, DHI
  proposes a dependent task in B (accept/decline) referencing the
  triggering change. Nothing is auto-created.
- Accepting a proposal creates a normal task, workflow-bound and
  worktree-isolated.

## Acceptance criteria

- [x] A task with two member changesets commits and pushes both, each on
      its own branch; a failure in one is named. (P0: `tasks.Commit`/
      `PushBranch` and `toolbridge.prOpen` iterate every changeset,
      collecting named per-member failures while the others proceed.)
- [x] Declared edges parse strictly; a dangling member warns by name.
      (P1: `workspace.toml [[dependency]]` strict-decoded — bad kind,
      self-edge, missing endpoint, duplicate all refuse; doctor
      `dependencies` row WARNs on a dangling member.)
- [x] Changing A with an A→B edge raises a proposal; declining creates
      nothing; accepting creates a linked B task. (P2: attaching a
      changeset seeds pending proposals from `ws.DependentsOf`; the
      inbox surfaces them; `a` accepts — creating `dep-<task>-<member>`
      and linking it — `x` declines. Re-seeding never resurrects a
      decided proposal.)
- [x] The dependency view renders declared edges. (P1: the REPOS
      section lists `from → to (kind)` edges and flags a missing
      member.)
- [x] `make verify` green per phase.

## Deferred

- Auto-detected dependencies (imports/APIs) layered over the declared
  graph.
- Cross-workspace (more than one workspace) operations.
