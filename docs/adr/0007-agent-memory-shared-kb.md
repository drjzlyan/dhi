# ADR-0007: File-based agent memory + shared knowledge base

Date: 2026-08-23 · Status: accepted

## Context

Agents must improve across sessions (private memory) and share knowledge
(workspace KB) without adding database dependencies to the hermetic runtime.

## Decision

Plain files under `.dhi/`: private per-agent memory at
`memory/agents/<id>/journal.jsonl` + `notes.md`; shared KB as markdown +
`knowledge/index.json` with provenance on every entry. Retrieval = managed
ripgrep + recency/importance scoring behind a `KnowledgeStore` interface, so
an embedding-backed implementation can replace it later. Contributions obey a
per-workspace policy (`auto` | `review`, default review).

## Consequences

- Diffable, portable, zero-dependency; consistent with repo-persisted knowledge.
- Semantic search quality initially limited to keyword+scoring.
