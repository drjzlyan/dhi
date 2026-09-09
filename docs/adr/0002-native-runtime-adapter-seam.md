# ADR-0002: Native agent runtime behind an adapter seam

Date: 2026-08-23 · Status: accepted · Superseded by: ADR-0013 (native
in-process turn engine removed — CLI runtimes only)

## Context

Agents must chat with the human and each other, share worktrees, and execute
tools inside DHI's sandbox. Delegating to external CLIs (claude/codex/gemini
processes) would make inter-agent messaging and deterministic testing hard.

## Decision

Build a **native in-process runtime** (orchestrator + provider interface +
tool executors). Define interfaces so external coding CLIs can be added later
as just another `Provider`/agent adapter.

## Consequences

- Full control of message bus, permissions, memory, and observability.
- All runtime behavior testable offline via scripted MockProvider.
- More upfront work than shelling out to existing CLI agents.
