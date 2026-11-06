# F-174 — Multi-cursor

Status: done · gap G-E13

The buffer had one cursor. Renaming a local, editing a column of lines, or wrapping
every match meant a substitute or a macro.

## Decision
- ctrl+d is half-page down here, so adding the next occurrence is `gb` (chord, next to
  `gc`/`gd`) with `alt+n` as the one-key form; `gB` skips the newest match. alt+n does not
  clash with F-117's alt digits or F-157's terminal-only alt+k/j.
- Cursor above / below is `alt+up` / `alt+down`. Terminals that swallow alt+arrows have
  `:cursor above|below|next|skip|off`.
- A key runs once at every cursor, last position first, then the other cursors are carried
  across the edit by diffing the text before and after. No hook in the editing primitives,
  so every command (motions, `d c y x D C p o O`, counts, text objects, insert-mode typing,
  autopair, enter, backspace, delete, paste, `.`) works at every cursor.
- Each cursor keeps its own register, so `yw` then `p` pastes what each took.

## Behaviour
- `gb` on a word adds a cursor at the next whole-word match (same offset in the word),
  wrapping; from a one-line visual selection it matches that text literally and leaves
  visual mode. `gB` drops the newest and takes the next. "no more occurrences" when done.
- One key (and one insert session) is one undo step. `u`, `ctrl+r`, `:`, `/`, `?`, `n`, `N`,
  `*`, `v`, `V`, `ctrl+v` and paging collapse to the primary cursor first.
- Cursors landing on the same spot merge; a cursor inside text another cursor's edit removes
  is absorbed. Any text change forgets the needle.
- `esc` in normal mode (no half-typed command) collapses to the primary; the surface lets
  that esc come before the one that leaves the buffer.
- The title shows `N CURSORS`; extra cursors use `theme.SecondaryCursor()`.
- With one cursor nothing changes: the only added work is a nil check and a five-key switch.

## Acceptance
- Commands at every cursor, with line-count changes above others:
  `TestMultiCursorCommands`, `TestMultiCursorFollowsEdits`.
- Add / skip / wrap / whole word / selection: `TestAddNextOccurrence`, `TestVerticalCursors`.
- Collapse, undo, merging: `TestMultiCursorEscUndoMerge`.
- Single-cursor path: `TestSingleCursorPathUntouched` and every existing textbuf test.
- Render and keys through the surface: `TestMultiCursorGolden` (`editor_multicursor_100x30`),
  `TestMultiCursorEscCollapsesBeforeLeaving`.
