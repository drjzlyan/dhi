# ADR-0003: Anthropic-first provider with scripted MockProvider

Date: 2026-08-23 · Status: accepted

## Context

Agent features need LLM access, but every test must run offline, fast, and
deterministically.

## Decision

Define one narrow `Provider` interface at the runtime boundary. Ship two
implementations initially: **Anthropic** (first real adapter) and a
**MockProvider** whose conversations are scripted in tests. Multi-provider
support (OpenAI-compatible, Ollama/local) arrives as additional adapters only
after M3.

## Consequences

- No network or keys needed anywhere in CI.
- Provider drift caught by interface tests, not production surprises.
