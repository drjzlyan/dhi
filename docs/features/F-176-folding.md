# F-176 — Code folding

Status: done · gap G-E9

Long functions could not be put away. Folds are a view concept: buffer text, undo, saves, search and
LSP positions never change.

## Decision
- Ranges come from `textDocument/foldingRange` when the server advertises it (client sends
  `lineFoldingOnly`). Otherwise an indentation fallback, named in every fold message
  (`folds from indentation (no language server for this file)` / `(<server> offers no folding ranges)`).
  A server that offers folding but has not answered shows no folds and says `waiting for the server`;
  the fallback is never substituted silently (ADR-0011). If the request fails, the fallback takes over
  and the source says `indentation (<server> folding failed: ...)`.
- State is per buffer (`textbuf.Buffer`), not per pane: panes of one tab share one cursor, and j/k and
  the operators run on that cursor, so per-pane folds would make `j` depend on the focused pane.
- Multi-cursor: closing a fold collapses every cursor inside it onto the fold's first line (cursors
  that meet merge); a cursor that jumps into a closed fold (search, `gb`, goto) opens it.
- Gutter: the single marker cell (before the text) shows `▸` on every closed fold (it beats a git
  mark; a breakpoint beats it) and `▾` on the cursor line's open fold when the cell is free. Showing
  `▾` on every fold start would change every existing golden. Clicking the marker toggles.

## Behaviour
- `za zc zo zA zC zO zM zR zj zk` (counts on zc/zo/zj/zk). zj/zk take counts and work after d c y gc
  (linewise, up to the target fold). They are not "." changes and run once with several cursors.
- A closed fold draws its first line plus a dim `⋯ N lines`. j/k, counts, `dd yy cc >> gcc`, `dj dk dG`,
  `V`, `x D dl` treat it as one line (vim-compatible; checked against vim 9.1); `o`/`p` land below it.
- A search, jump, insert, or an edit touching hidden lines opens the folds around it. Edits above
  shift folds (diff of lines against a snapshot); a fold that loses its first line is dropped.
- Composes with wrap (a closed fold is one row), inlay hints, blame, sticky scroll (cursor row is
  counted in drawn rows), clicks (row to line map).

## Acceptance
- vim-checked composed forms: `TestFoldKeysMatchVim`, `TestFoldOpenCloseCommands`,
  `TestFoldVisualLineTakesWholeFold` (textbuf).
- Edits and invalid ranges: `TestFoldMapsAcrossEdits`, `TestFoldSetRangesKeepsClosedStarts`.
- Cursors: `TestFoldSecondaryCursorsCollapseOnClose`, `TestFoldMultiCursorOpensOrCollapses`.
- LSP: `TestFoldingRangesDecode`, `TestFoldRangesFromTheServer`, `TestFoldServerFailureFallsBackNamed`,
  `TestFoldServerWithoutCapabilityIsNamed`, live `TestLiveGopls/folding_ranges`.
- View: `TestFoldGolden` (`editor_fold_closed_100x30`), `TestFoldInWrapMode`, `TestFoldClicks`,
  `TestFoldSearchLandingOpens`, `TestFoldSharedBetweenPanes`.

## Open
- Git marks and diagnostics inside a closed fold are not summarised on its row.
