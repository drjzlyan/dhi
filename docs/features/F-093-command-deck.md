# F-093 — Command deck: a bold UI/UX refresh

Status: in progress (M33) · Decision record: ADR-0040 · Builds on: F-025, F-026, F-041, F-064

## Why

The user asked for a UI that is "extremely beautiful and engaging" with great UX. They chose a bold
refresh across every surface, in the "command deck" direction. Colored captures of the real app
(`scripts/screenshots.sh`, all three themes, 80×24 and 120×36) show what holds DHI back:

- **Boxes inside boxes.** The shell, the section panel and the inset columns each draw a frame, so
  the content sits three frames deep.
- **Hints appear twice and get clipped.** Every pane has a footer keymap that is cut mid-word
  (`chann…`, `ct…`, `/ …`). The status bar repeats the global keys underneath. Composers add a third
  hint line, and editor buffers float `i insert · : cmd` in the text area.
- **The tab bar reads as a path.** Tabs are joined with `›`. The status bar opens with a cryptic red
  `!1`.
- **Rough copy.**
  - `(s)` plurals: `0 turn(s)`, `3 task(s)`, `1 draft(s)`.
  - Parenthesised empty text that is clipped instead of wrapped: `(no messages yet — … an a`.
  - `0` counts in the rails.
- **Truncation and drift.** Board cards are truncated to about 15 characters while two thirds of the
  board is empty. Time columns drift when an inbox row wraps. Table columns are clipped (`la`).
- **A glyph some fonts lack.** The wizard stepper uses `◐`, which renders as a tofu box in common
  fonts (seen in the freeze capture).

## Behaviour

### Chrome

- **Top bar.**
  - `◆ dhi`, then the surfaces as pills: the active one is filled with the surface hue and each tab
    carries its number.
  - No `›`.
  - The presence chip sits on the right.
- **Status bar.** One row that owns every hint:
  - A mode block in the surface hue (`WORKSPACE`), then the zone, then a labelled attention pill
    (`◆ 1 waiting`).
  - The flash message.
  - The active surface's hints, most important first. Hints that do not fit are dropped whole and
    `? more` takes their place.
  - The global keys come last and are dropped first.
- **Panes are a deck, not boxes.**
  - The header row has a bold title and a rule that fills the rest of the row. On the focused pane
    the rule is a cyan→violet gradient; elsewhere it is quiet.
  - A thin `▏` divider runs down the left edge. There are no corners, side walls or bottom wall.
  - Dialogs, the palette, help and the boot gates keep their boxes.
- **Rails.** Zero counts are hidden, and the attention count is a tinted pill.

### Primitives

- `kit.Count(n, noun)` handles plurals.
- Empty states use the shared `kit.EmptyState` / `kit.EmptyInline`, which wrap and never clip.
- Composers show their send hint inline only while focused.

### Surfaces

- **Workspace.** No pane footers. Wider board cards whose titles wrap. Inbox time columns stay put.
- **First run.** Font-safe stepper glyphs and a card layout for the wizard.
- **Editor.** No floating hint lines in buffers. One mode pill per pane.
- **Settings, Reviewer, Ideator.** No pane footers, plural-correct copy, shared empty states.

## Acceptance

- [ ] No hint is rendered twice on screen. No hint is clipped mid-word: whole hints drop and `? more`
      appears.
- [ ] No `(s)` in any rendered TUI string. No `0` count in any rail.
- [ ] Every empty pane uses `kit.EmptyState`/`EmptyInline` and wraps at 80×24.
- [ ] At most one framed level on screen. Boxes only for dialogs, palette, help and gates.
- [ ] Time and meta columns stay aligned when a row wraps.
- [ ] New tokens and glyphs work in Dark, Light and High contrast. `contrast_test` passes. Glyphs are
      font-safe (no `◐`, `⎇`).
- [ ] Every animation is gated on `theme.Motion` and has a reduced-motion golden.
- [ ] `AssertBgIntegrity` passes on new primitives and surfaces.
- [ ] The F-041 typing-safety tests stay green.
- [ ] Nothing new in config files: any knob is a Settings row.
- [ ] Colored after-captures (3 themes, 80×24 and 120×36) are reviewed against the baseline.

## Tooling

`scripts/screenshots.sh` takes `DHI_SHOT_THEME`, `DHI_SHOT_SIZE` and `DHI_SHOT_OUT`, so review
captures never overwrite the README assets. It also launches once before shooting. That launch
accepts the one-time repairs and skips setup in the sandbox home, so every shot shows the app and
not the repair gate or the wizard.
