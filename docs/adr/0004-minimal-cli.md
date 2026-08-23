# ADR-0004: Minimal CLI — the IDE is the interface

Date: 2026-08-23 · Status: accepted

## Context

Project/workspace management could live in shell subcommands (`dhi init`,
`dhi ws add`) or inside the TUI. CLI-heavy designs split the product in two
and demand a second testing surface.

## Decision

The `dhi` binary exposes **only**: bare launch, `dhi doctor [--json]`
(environment audit), `dhi version`. All workspace/project operations are
in-TUI flows (command palette forms), tested by scripted-key tests like any
other surface. `--json` makes doctor CI-consumable.

## Consequences

- One interaction model; one test harness.
- Scripted automation of *setup* waits until palette flows exist (M1/M2).
