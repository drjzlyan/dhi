# ADR-0021: Cross-project dependencies — declared edges, proposed propagation

Date: 2026-09-26 · Status: accepted · Companion to: ADR-0009 (worktrees),
ADR-0011 (no silent fallbacks), F-021 (workspace dashboard). Serves:
F-032.

## Context

A workspace already holds N member repos and a task can record multiple
`[[changeset]]` members — but every operation (agent cwd, commit, push,
PR) uses only the **first** changeset, so multi-project work is a
correctness hole, not a feature. The product goal says projects may
communicate directly (explicit hand-off) or indirectly (shared repo
contracts). Indirect communication is invisible today: nothing knows
that changing repo A's interface affects repo B.

## Decision

1. **Honor all changesets first.** A task may carry one worktree per
   member; agent cwd, commit, push, and PR operate across all of them,
   each on its own branch. This is a correctness fix and the foundation
   for everything below; it ships before any dependency feature.
2. **Dependencies are declared, not guessed.** `workspace.toml` gains
   `[[dependency]] from = "<member>" to = "<member>" kind = "module |
   api | build"`. Declared edges are strict-decoded; a dangling member
   is a named doctor warning. Auto-detection from code is deferred until
   declared edges prove the model.
3. **Propagation is a proposal, never silent fan-out.** When a task
   changes member A and a declared edge A→B exists, DHI **proposes** a
   dependent task in B — a visible, accept/decline item in the board
   inbox — carrying a reference to the triggering change. Nothing is
   created without acceptance (ADR-0011 spirit).
4. **The graph is a first-class surface.** Declared edges render as a
   workspace dependency view (inspect / board context), so the crew can
   see what a change touches before accepting a proposal.

## Consequences

- `internal/workspace` gains the dependency list; `internal/tasks` gains
  multi-changeset operations; the proposal queue reuses the inbox
  aggregation pattern.
- Cross-project work stays worktree-isolated and review-gated per
  member; propagation does not weaken isolation.
- Detection (imports/APIs) is a future layer over the same declared
  graph, not a replacement for it.
