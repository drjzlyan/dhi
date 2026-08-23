# F-001: App shell & design system foundation

Status: **done** (2026-08-23) · Milestone: M0

## Summary

The DHI shell: themed design tokens, reusable kit primitives, surface
registry with keyboard routing, help overlay, statusline — plus the golden
snapshot harness all future UI work builds on.

## Acceptance criteria (all met)

1. `go run ./cmd/dhi` launches full-screen with branded tab bar, Home
   dashboard, statusline; alt-screen cleaned up on exit.
2. Keys: `1-9` jump, `tab`/`shift+tab` cycle (wrap-around), `?` toggles help
   overlay, `ctrl+c` quits. Unknown keys forward to the active surface.
3. All colors/metrics/glyphs come from `internal/tui/theme`; a lint test
   fails on raw color construction elsewhere.
4. Kit primitives (Panel/Tabs/StatusLine/List) render deterministically and
   pad exactly to requested widths.
5. Golden snapshots locked for kit components + two full-shell frames at
   100×30; regenerate via `DHI_UPDATE_GOLDENS=1`.
6. `make verify` green: fmt + vet + build + tests (incl. `-race` in CI).

## Out of scope (deliberate)

Real surfaces (M2+), user config/theming switch (M2), animation drivers (M1),
teatest program-level E2E harness (M2).
