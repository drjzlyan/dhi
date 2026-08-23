# ADR-0008: Git operations via go-git; no managed git binary

Date: 2026-08-23 · Status: accepted

## Context

ADR-0005 pins every dependency in the hermetic prefix, but git has no
official per-platform binaries. The candidates — community static builds,
conda-forge relocatable tarballs, or silently using host git — each add
supply-chain trust in a non-official builder, extraction complexity
(prefix rewriting), or an isolation exception.

## Decision

DHI implements its own git operations with **go-git (pure Go)** for the
Git view and agent-driven repo work (M2+): status/stage/commit/log/
branches/worktrees happen in-process behind a `gitcore` service seam.
No `git` entry exists in the registry manifest. A real git binary may
still be used *explicitly* by users inside terminal drawers (their own
host tooling), never implicitly by DHI features.

## Consequences

- One fewer download (~40MB) and one less non-official trust anchor.
- Feature parity gaps vs C-git are handled per-feature at the `gitcore`
  seam; anything go-git cannot do ships as a visible limitation, not a
  silent fallback to host tools.
- If parity ever demands it, revisiting means a new pin + ADR, reviewed
  like supply-chain code.
