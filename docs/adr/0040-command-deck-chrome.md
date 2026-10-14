# ADR-0040: Command deck — hints live in the status bar, panes have no boxes

Date: 2026-10-14 · Status: accepted · Supersedes in part: F-025 Part A (pane hint bars), F-026 P1
(boxed `Panel` as the unit of every surface). Keeps: F-064 background integrity, splice-only
overlays, the shade ladder. M33, F-093.

## Context

F-025 placed each surface's keymap on a hint bar at the bottom of its pane. The shell's status bar
then repeated the global keys. Every pane was a rounded box, and sections nested inset columns inside
those boxes. Colored captures show what this costs:

- two rows of hints, both clipped;
- three frames deep before any content;
- a tab bar joined with `›`, so it reads as a breadcrumb.

The user asked for a bold refresh and chose the "command deck" direction.

## Decision

1. **One home for hints.** A surface reports its hints through `surfaces.HintProvider` (`Hints()`
   returns the flash message and the keymap, most important first). The shell's status bar renders
   them after the mode block, then the global keys. Overflow drops whole hints from the end, the
   global ones first, and shows `? more`. Surfaces no longer paint a hint footer in their panes.
2. **Panes are a deck.** `kit.Panel` renders a header row: the title, plus a rule that fills the row.
   On the focused pane the rule is an Accent→Accent2 gradient. A thin left divider runs down the
   pane, and there are no corners, right wall or bottom wall.

   The geometry is unchanged: one row on top, one column each side, one row at the bottom. Click
   origins and body budgets therefore stay exactly as they were. `Panel.Boxed` keeps the rounded
   frame for the boot and repair gates. Dialogs keep using `kit.Modal`.
3. **Tabs are pills.** They show the number and the label, the active tab filled with the surface
   hue. There is no separator glyph.
4. **Copy rules.**
   - `kit.Count` handles plurals.
   - Empty states use `kit.EmptyState`/`EmptyInline`.
   - Rails never show zero counts.
   - The attention segment is labelled (`◆ N waiting`).

## Consequences

- Hints are no longer next to the pane they act on. The mode block and zone in the status bar name
  that pane instead, and `?` help still lists every key, grouped.
- Clicks on hints move from pane footers to the status bar row. The shell maps a status-bar click to
  the key and hands it to the surface.
- On 256-color terminals the dark canvas and panel shades collapse to one color (tested). Panel
  and inset (pane vs rail) and every zone vs the selection stay distinct. Panes therefore keep the
  `▏` divider and the header rule as their boundary, not shade alone.
- Goldens change across every surface. Each phase regenerates them deliberately and reviews the
  diff.
