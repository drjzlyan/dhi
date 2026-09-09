# F-024: Theme & dialog refresh — backgrounds, borders, one modal system

Status: planned (M11 P1/P5) · Milestone: M11 · Depends on: nothing
(P1 lands first; surfaces adopt through P2–P5). Companion to ADR-0014.

## Summary

Two foundational fixes plus an aesthetic pass: (1) the kit Panel's
titled top edge is one column short — the `╮` corner sits inset from
the body's `│` (the "cut border"); (2) four independent modal systems
render dialogs inconsistently, and Settings renders forms inline
instead of overlaid. The refresh unifies both and re-skins section
layering with background shades of black/gray.

## Part A — panel corner fix (kit)

`kit/panel.go topEdge`: the titled branch computes
`fill := width - 2 - tw - 2`, so `2 + tw + fill + 1 = width - 1` —
one column short. Fix: `fill := width - 2 - tw - 1`. The untitled
branch, body rows, and bottom edge are correct. Every titled panel
golden regenerates once, deliberately (AGENTS rule 3).

## Part B — background shades (theme)

New/clarified tokens on the dark-futuristic palette (all near-black
grays; no raw `lipgloss.Color` outside `internal/tui/theme` — the
lint test stays):

| token | use |
|---|---|
| `BgBase` | app background (workspace body) |
| `BgPanel` | panel bodies (the existing token, kept) |
| `BgElevated` | modals, context pane, overlays |
| `BgInset` | sub-columns inside a panel: kanban columns, Slack rail, thread pane |
| `BgSelection` | cursor/selection fill (existing, kept) |
| `BgOverlay` | modal backdrop dim layer |

Style helpers gain per-surface selection: panel edges stay subtle
(`Border` unfocused / `BorderFocused` on the focused pane), section
headers and rail groups render dim, status words (backlog/active/
in-review/done) get quiet bg-chip styling.

## Part C — one dialog system (kit)

- **`kit.Modal`** — the single overlay primitive: renders over a
  backdrop of dimmed content lines, centered box with border + title,
  correct width math (inner width = box − 4), styled-clip preservation
  (clip keeps ANSI styles instead of stripping), focus-trap (all keys
  consumed while open), `esc` close contract, error line + busy
  marker.
- **`kit.Form`** — labeled text fields + toggle fields (left/right
  cycles, tab next), built on Modal; the field model is lifted from
  the workspace `formState`/settings `agentForm` implementations.
- **`kit.Columns`** — generic N-column layout with one cursor per
  column (board + any future column view); app-agnostic.

Adoption: workspace, settings, ideator, reviewer replace their
`modalKind`/`stackOver`/`overlayCentered`/inline-form rendering with
Modal/Form (P2–P5); the app help overlay becomes a true Modal over
the body instead of replacing it. No behavior change beyond rendering:
the same keys submit/cancel, the same strict validation, the same
refusals.

## Acceptance criteria

- Titled panels are exactly `width` wide on every row (top edge =
  body = bottom); `panel_focused`/`panel_unfocused` goldens show the
  continuous corner.
- Modal: renders over dimmed backdrop, box centered, wide content
  clips with styles intact, esc/enter/tab behave, keys never leak to
  the surface beneath while open.
- Form: tab cycles, ←/→ cycles toggles, enter submits, esc cancels,
  busy swallows input, errors render in DangerText.
- Theme lint (no raw colors outside theme) stays green; every new
  token has a Dark + Light value (tokens-complete test).
- `make verify` green; all goldens regenerated deliberately and
  reviewed.
