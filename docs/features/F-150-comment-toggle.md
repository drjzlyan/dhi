# F-150 — Comment toggle

Status: done · gap G-E10

The editor had no way to comment code out: `gcc`, `gc{motion}`, visual `gc` and `ctrl+/` did nothing.

## Decision
- The comment syntax lives in `textbuf` (`CommentFor`, by extension or file name), not in the LSP
  language table, because CSS, HTML, Markdown and Makefile have no language server. A file type
  without comments (JSON) or unknown says so on the status line (ADR-0011); nothing is guessed.
- Block-comment-only types (CSS, HTML, XML, Markdown) wrap each line in the pair, so the toggle is
  per line and round-trips.
- `gcip` and the other text objects work (F-167).
- `ctrl+/` has two key names: terminals send `ctrl+_` for it, both are handled. In the editor
  buffer they no longer open help (the app's global `?`/`ctrl+/` help only applies outside buffer
  focus, where `?` is search); help stays on `?` outside the buffer (`esc` returns to the tree).

## Behaviour
- `ToggleComment(lines, Comment)` (pure): if every non-blank line is commented, uncomment all (token
  and one following space go); otherwise comment all non-blank lines at the shallowest common
  indent. Blank lines are untouched; a range of blanks is a no-op.
- Normal: `gcc` (count lines: `3gcc`), `gc{motion}` (`gcj`, `gck`, `gcG`, `gcgg`, `gcw` comments the
  line), `gcgc`, `ctrl+/` (count lines). Counts multiply like vim (`2gc3j`); the same fix applies to
  `d`, `c`, `y`.
- Visual (`v`/`V`): `gc` or `ctrl+/` toggles the selected lines and returns to normal.
- Insert: `ctrl+/` toggles the current line and keeps the cursor in the same text.
- One undo step per command; `.` repeats it; status line says "N lines commented/uncommented".

## Acceptance
- Pure transform: `TestToggleComment`, `TestToggleCommentRoundTrip`, `TestCommentFor`.
- `gcc`/counts: `TestGccLineAndCount`; motions: `TestGcMotion`; `gcgc`/esc: `TestGcDoubledAndAbandoned`.
- Visual: `TestGcVisual`, `TestCtrlSlashVisual`; undo and `.`: `TestGcOneUndoStepAndDot`,
  `TestGccDotRepeatsOnNextLine`.
- `ctrl+/`, `ctrl+_`, insert: `TestCtrlSlash`, `TestCtrlSlashInsertKeepsTyping`.
- No syntax / block types: `TestCommentWithoutSyntaxSaysSo`, `TestCommentBlockLanguageEditor`.
- Through the surface's g-chord: `TestCommentChordsThroughSurface`.
