# ADR-0041: One DHI identity, component roles, Editor first, setup as a checklist

Date: 2026-10-20 · Status: accepted · Supersedes in part: F-055 (per-view identity hue), ADR-0040
§3 (tab and mode block in the view's hue), ADR-0026 §4 (setup wizard auto-runs as a gate). Keeps:
ADR-0040's deck panes and the status row as the only place for hints, the shade ladder (F-064),
and ADR-0011 (boot blockers still gate). M34, F-095–F-100.

## Context

The palette was the cyan/violet "dark-futuristic" set. Each view had its own hue, and the Ideator
and Settings borrowed semantic colors for it: amber, which is Warning, and dim text. So the attention
pill and the Ideator mode block looked the same, and a hue meant both "where you are" and "something
is wrong". Controls were not drawn as controls: a form field was a dim label and `[value]`, a button
was a keycap, and checkboxes were hand-drawn glyphs. The app opened on the Workspace. First-time users
went through an eleven-step wizard gate before they saw anything.

The user asked for proper UI components with their own colors from one new DHI brand identity, kept
the same in every view. They also asked for the Editor as the first screen, and for onboarding that is
interactive and helpful.

## Decision

1. **One brand identity.** "dhī" means insight, so the palette is a saffron lamp on indigo ink:
   - saffron is the primary and indigo the secondary
   - lotus and teal are tertiary hues for authors, file kinds and chips
   - semantic colors (success, warning, danger) are kept apart from the brand hues, and a test
     enforces the distance (ΔE)

   The themes are `dhi-dusk` (dark), `dhi-dawn` (light) and `high-contrast`. The old names
   `dark-futuristic` and `light-paper` remain as aliases, so existing configs still load.
2. **Components own their color; views do not.** `theme.Tokens` gains component roles: button
   variants, input, card, tab, badge, focus and link. Every view uses the same roles. You can tell
   which view you are in from the active tab and the mode-block label, not from a hue.
3. **Real controls in `kit`.** Button, Input, Toggle/Checkbox/Radio, Badge, Card and Checklist.
   Each one is drawn with fills (no extra frames). A Form row stays one row high, so existing
   geometry and click origins are unchanged.
4. **The Editor is view 1.** It opens on a start page with actions, the setup checklist and the
   tour. Digits switch views wherever no buffer has focus. `alt+1..9` switches views from anywhere,
   except a focused terminal, which keeps `alt+1..9` for its own tabs.
5. **Setup is a checklist, not a gate.** Boot, bootstrap and repair gates still block, because they
   are real blockers. The setup wizard no longer auto-runs. The start page and a `setup N/M` status
   pill list the steps that apply. Each step runs on its own as a focused dialog (`wizard.Single`),
   records only that step, and never marks setup finished. The linear wizard stays in the palette.

## Consequences

- Goldens change across every surface. Each phase regenerates them deliberately.
- Existing users whose setup is `Finished` with no per-step records see only the catch-up steps
  that still apply, the same as today's catch-up gate.
- Tutorial lessons and screenshot scripts that used view numbers are renumbered.
- The one-time welcome card is retired; the start page replaces it.
