# F-136 — Soft word wrap in the editor

Status: done · gap G-E4

Long lines ran off the pane and scrolled sideways (F-119). Some people want to read prose,
Markdown and logs without moving sideways.

## Behaviour
- Off by default. Turn it on in Settings (`editor.wrap`, "wrap long lines"; live, like tab
  width) or for the session with `:set wrap`; `:set nowrap` turns it off. The `:set` choice
  beats the setting until DHI restarts.
- A long line continues on the next rows at the pane width. A row breaks after the last space
  that fits, or mid-word when a word is longer than the row. Indentation is never a break
  point, and a wide (east-asian) rune is never split.
- The gutter shows the line number, git mark and breakpoint only on a line's first row;
  continuation rows have a blank gutter.
- `j` and `k` stay on logical lines. The cursor, visual selection, search marks and syntax
  colors render on whichever row holds them.
- There is no horizontal scroll while wrap is on.
- Scrolling counts rows: the window keeps the cursor row in view (a line taller than the pane
  follows the cursor). Clicks and drags map a row back to its line and column. Popups hang
  under the cursor's row.
- `editor.wrap` is a known key in `config.toml` and an agent-proposable setting like
  `editor.line_numbers`.

## Acceptance
- Break points: `TestWrapSegments`.
- Wrap off is unchanged: `TestWrapOffKeepsHorizontalScroll` plus every existing golden.
- Number on the first row only, nothing cut off: `TestWrapContinuesLongLinesAndNumbersFirstRowOnly`,
  `TestWrapBreaksAtSpaces`.
- `:set wrap` / `:set nowrap`: `TestSetWrapAndNoWrapOverrideSettings`.
- `j`/`k` logical: `TestWrapJKStayLogical`. Cursor and scroll: `TestWrapCursorOnLaterRowStaysVisible`,
  `TestWrapScrollsByRowsKeepingCursorInView`, `TestWrapCursorLineTallerThanPane`,
  `TestWrapEndOfLineCursorGetsACell`.
- Clicks: `TestWrapClickMapsRowToPosition`. Wide runes: `TestWrapWideRunesStayWhole`.
- Snapshot: `TestWrapGolden` (`editor_wrap_100x30`).
- Setting: `TestEditorWrapLayer`, `TestWrapToggle`.
