# F-159 — Block visual mode (ctrl+v)

Status: done · gap G-E14

Visual mode selected only characters or whole lines. Column edits (add a prefix to ten lines,
delete a column, align a block) had no tool.

## Decision
`ctrl+v` is free in the editor (it was only the second key of the `ctrl+w` split prefix), so it
is the one binding. vim's `ctrl+q` fallback is not offered: `ctrl+q` always quits DHI (app level).
A terminal that swallows `ctrl+v` for paste cannot reach block mode; the bubbletea paste event
is separate, so most do not.

## Behaviour
- The block is measured in display cells: a tab is as wide as the next tab stop
  (`editor.tab_width`, pushed in with `SetTabWidth`), a wide rune is two cells. A character that
  straddles a block edge is split into spaces, like vim.
- Motions extend the block; `$` makes it reach each line's end and `j`/`k` keep that; any
  horizontal motion ends it. `o` swaps corners, `O` swaps the columns on the same line.
  `v`, `V`, `ctrl+v` switch the kind of selection (the same key leaves visual mode).
- `d` `x` delete the block, `D` deletes to each line's end, `y` yanks it as a block register
  (cursor to the top-left). `p`/`P` paste it as a rectangle at the cursor's column: short target
  lines are padded with spaces, new lines are added at the end, `3p` repeats it side by side,
  and padding after the text is added only when text follows.
- `c`/`C`, `I`, `A` (`A` with `$` appends at each line's end): the text typed on the first line
  is copied to the other lines on esc. Lines too short to reach the block are left alone (`A`
  pads them). `r{c}` fills the block, `>` `<` shift the text right of the block's left edge by
  one indent unit (count repeats), `~` `u` `U` change case.
- Every operation is one undo step (the delete and the typing of `c`/`I`/`A` share a group).
  `.` replays the keys, so it repeats a same-size block at the cursor, as in vim.
- `Selection()` in block mode returns the block text (lines joined by newline) with the
  enclosing corners.
- The view inverts the block's cells on each line (short lines only where they have text); the
  mode chip reads `V-BLOCK`. Help lists the key under the buffer section.

## Acceptance
- Delete / yank / paste / change / insert / append / replace / shift / case rectangles, `$`,
  short lines, corners: `TestBlockOps` (table).
- Tabs and wide runes straddling an edge: `TestBlockSliceTabsAndWide`, `TestBlockByDisplayColumn`.
- One undo step and dot repeat: `TestBlockUndoAndDot`.
- Mode switching and range: `TestBlockModeState`; a linewise yank replaces the block register:
  `TestBlockPasteNeedsBlockRegister`.
- Rendering: `TestBlockVisualGolden` (`editor_block_visual_100x30`), `TestMarkCells`.
