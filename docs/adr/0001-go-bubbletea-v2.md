# ADR-0001: Go + Bubble Tea v2 as the DHI stack

Date: 2026-08-23 · Status: accepted

## Context

DHI is a highly interactive TUI IDE needing: deterministic UI testing,
streaming agent concurrency, aesthetic compositing, single-binary
distribution, and fast iteration.

Candidates evaluated: Rust+ratatui (strong typing, slower iteration),
TypeScript custom renderer (richest AI SDKs, weakest TUI perf/distribution).

## Decision

**Go with Charm v2**: `charm.land/bubbletea/v2` (Elm-architecture models map
directly to testable state machines), `charm.land/lipgloss/v2`
(advanced compositing for the design system), plus teatest for program-level
E2E later. Goroutines fit agent orchestration naturally.

Note: v2 module paths are `charm.land/*`, **not**
`github.com/charmbracelet/*`; `Model.View()` returns a `tea.View` struct
(`tea.NewView(s)`, `.AltScreen = true`) and keys arrive as `tea.KeyPressMsg`.

## Consequences

- Fastest path to the testable component library M0 requires.
- Locked to Go toolchain ≥1.26; charm.land import paths everywhere.
