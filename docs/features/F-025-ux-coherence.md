# F-025: Coherent UX — bottom chrome, shaded zones, full-space layouts

Status: implemented (M12, 2026-09-10)
Modal/Form/Columns, bg tokens). Decisions (2026-09-10): keymap
instructions move to the BOTTOM of every pane on a dedicated chrome
background; Settings adopts the left-rail IA; the statusline becomes
contextual; narrow terminals get full-width stacks.

## Summary

The surfaces grew independently: four hand-rolled rails (only the
workspace one shaded), keymap hint rows scattered at the top of pane
bodies, a static statusline, and narrow fallbacks that center content
inside dead margins. F-025 makes one system: every surface fills the
terminal, zones are separated by background shades, and key
instructions live in a quiet chrome bar pinned to the bottom — visible
when you need them, never the focus.

## Part A — bottom chrome (the keymap rule)

- **`kit.HintBar`** — one row pinned to the foot of every main pane:
  optional status/flash segment (semantic color) then muted keymap
  segments (`n new · e edit · …`), right-padded on a dedicated
  **`theme.BgChrome`** background — a shade distinct from Bg/Panel/
  Inset/Elevated so the instruction zone reads as chrome, not content.
- Per-surface top-of-body keymap rows are deleted (board, inbox,
  repos, ideator, reviewer, settings foot, channels composer hint).
  Content starts at row 1; the HintBar is always the last pane row.
- Flash/status messages render on the same bar (left segment,
  success/warning/danger fg on the chrome bg) — one place announces
  outcomes.
- The bar is single-row, muted (`TextDim` on BgChrome), identical in
  position across surfaces: instructions are findable by reflex, never
  the visual focus.

## Part B — shaded zones

- **`kit.Rail`** — the nav sidebar primitive (inset-shade rows as
  single-styled strings — SGR resets inside concatenated segments drop
  row backgrounds): title, rows with counts/badges, active highlight,
  optional foot. Adopts the workspace rail; replaces the hand-rolled
  ideator/reviewer rails; new in Settings.
- Editor: files rail rows get the inset shade; crew chat/git/terminal
  panels keep their borders and gain quiet zone shading where rows are
  plain.
- Board: active lane header chip, per-lane status color dot
  (generic `kit.Column.Accent`), detail fact pane on ElevatedBg.
- Workspace/ideator/reviewer zone headers (title + summary counts) may
  claim pane row 1 — headers only, never keymaps.

## Part C — contextual statusline (bottom, unchanged position)

- Left: mode chip (e.g. ` INSERT `, ` FIND `, ` CHAT `; accent on
  selection bg) + context `surface › zone` (e.g. `Workspace › board`).
  Sourced via narrow interface assertions — `StatusContext() (zone,
  mode string)` and optional `StatusHints() []string` — so the
  Surface contract is untouched and surfaces without them degrade to
  today's static line. Recomputed per frame (mode changes render live).
- Right: active surface's key summary (max 3) + the global keys
  (`1-5 views · tab · ? help · ^c quit`).
- The static ` NORMAL ` dies.

## Part D — responsive

- Shared breakpoints in kit: `WCompact=60`, `WDock=84`, `WWide=120`.
- Below the dock breakpoint surfaces render a **full-width vertical
  stack** (section strip on top, body filling the width, HintBar foot)
  — no centered dead margins. `kit.Center` survives only under 60 cols.
- Every surface returns exactly width×height cells; audit per surface.

## Acceptance criteria

- No keymap text renders above the first content row of any pane; each
  main pane's last row is a BgChrome HintBar with that section's keys.
- Rails (workspace/ideator/reviewer/settings) share one primitive and
  the inset shade; editor files rail rows are inset-shaded.
- Statusline shows `mode chip + surface › zone` live (switch section in
  workspace / focus a buffer in editor → the line updates without a
  surface switch) and per-surface hints on the right.
- Narrow (60–83 cols): full-width stack, zero side margins; <60: the
  centered fallback remains.
- No key behavior changes; the editor chat sidebar behavior unchanged.
- Theme lint stays green (BgChrome has dark+light values); all goldens
  regenerated deliberately.
- `make verify` green.

## Deferred

- ~~Tab-bar badges~~ — **closed (not planned)**: the statusline `!N`
  carries it.
- ~~Mouse support~~ — **closed (landed)**: F-026 P2 enables mouse
  (wheel + click, no drag) with nil-safe `Wheel`/`Click` seams.
- Per-pane scrollbars: `kit.Panel.SetScroll` paints a thumb on the
  pane's right edge from a `kit.NewScroller(total, height, offset)`
  when the window overflows. Landed for the reviewer DIFF + run
  transcript and the workspace REPOS pane (which now has a real scroll
  window with cursor-follow). INBOX/BOARD/FILES still clip rather than
  scroll, so they adopt as they gain windows.
- ~~Reworking the editor's centered empty-state~~ — **closed (not
  planned)**: intentional hero.
