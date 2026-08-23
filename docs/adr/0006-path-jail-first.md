# ADR-0006: Path-jail sandboxing first, OS sandbox behind a seam

Date: 2026-08-23 · Status: accepted

## Context

Agents execute fs/shell/git operations against user code. Full OS-level
sandboxing (bubblewrap/seatbelt) from day one would drag platform edge cases
into core milestones.

## Decision

Foundation ships **path-jail + policy**: operations resolve through the VPath
registry and may only touch registered workspace paths; dangerous ops are
deny-by-default via config-driven permission policies. Define a
`sandbox.Sandbox` interface now so bubblewrap (Linux) / seatbelt (macOS)
adapters wrap every op later without touching call sites.

## Consequences

- Core milestones stay unblocked; isolation strengthens incrementally.
- Policy engine doubles as the audit surface for agent actions.
