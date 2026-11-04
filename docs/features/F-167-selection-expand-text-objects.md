# F-167 — Selection expand/shrink and vim text objects

Status: done · gap G-E19

The editor could neither grow a selection by structure (VS Code shift+alt+→, Helix alt+o) nor
take vim text objects (`ciw`, `di(`, `yip`, `gcip`), so G-E10's comment operator had no `ip`.

## Decision
- Keys: `alt+o` expands, `alt+i` shrinks, in normal and visual mode (Helix's pair). VS Code's
  shift+alt+arrows reach a terminal as ambiguous escape sequences and collide with word
  movement; `+`/`-` in visual mode would shadow vim motions. alt+o/alt+i were free in the
  buffer (alt+o only moves between terminal panes while a terminal has focus). On macOS,
  Option must be Meta (the hint renders as ⌥).
- Source of ranges: the language server's `textDocument/selectionRange` when it advertises
  `selectionRangeProvider`; otherwise the structural ladder. Never silent: the status line says
  `expand: language server`, or `expand: <rung> (structural: <why>)` where why is
  `no language server`, `server has no selectionRange`, `selectionRange failed: <err>` or
  `server has no larger range`.
- The ladder takes the smallest range that strictly contains the selection among: word, inside
  quotes, quotes, inside brackets, brackets (every nesting level, `( [ {`), line text (without
  indent), line, paragraph, buffer. Brackets match across lines (not syntax aware); a
  selection never ends on a line break.
- Shrink walks back the stack of earlier selections (ending in normal mode at the original
  cursor); moving the selection yourself starts a new chain.

## Behaviour
- Text objects `iw aw i( a( i[ a[ i{ a{ i" a" i' a' i` a` ip ap` (`b` = `(`, `B` = `{`) work after
  `d c y gc` (counts as in vim: `d2aw`, `2daw`, `d2i(`) and in visual mode, where repeating an
  object (`vi(i(`) grows to the enclosing one and `ip`/`ap` select linewise.
- `i(` on `()` does nothing for d/y and opens insert for c. A multi-line `i{` takes the lines
  between the braces, not the line breaks.
- Fixes found on the way: a linewise delete ending at an empty line no longer drops the rest
  of the buffer, and a linewise change before a blank line keeps that blank line.

## Acceptance
- Operators and visual forms, incl. counts, dot repeat, cancel: `TestTextObjectOperators`,
  `TestTextObjectCommentOperator`, `TestTextObjectDotRepeat`, `TestTextObjectUnknownObjectCancels`,
  `TestTextObjectVisual`, `TestTextObjectVisualOperators` (textbuf).
- Ladder rungs, names, shrink, broken chain: `TestStructuralLadder`, `TestStructuralLadderRungNames`,
  `TestShrinkWalksBack`, `TestShrinkChainBrokenByOtherMoves`, `TestLadderEdges` (textbuf).
- selectionRange client: `TestSelectionRangesFlattensParents`, `TestCanSelectionRange` (lsp).
- Fake server end to end and each fallback message: `TestExpandUsesServerSelectionRange`,
  `TestExpandFallsBackWhenServerLacksSelectionRange`, `TestExpandFallsBackOnServerError`,
  `TestExpandFallsBackWhenServerRangeNotLarger`, `TestExpandWithoutLanguageServerNamesTheRung`,
  `TestTextObjectKeysThroughSurface` (surface, incl. `gcip` through the g-chord).
