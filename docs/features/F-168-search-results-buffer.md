# F-168 — Search results buffer

Status: done · gap G-E20

The results pane is a list: it can jump and run a project replace, but you cannot read matches with
context, edit several of them by hand, or keep them while you work (VS Code's search editor).

## Decision
The listing is an ordinary editor tab on `.dhi/tmp/SEARCH_RESULTS` (DHI's own folder, like the commit
message buffer), so every editing key works. `:w` never writes source files by itself: it opens a
confirm card, and the write goes through `projreplace.Apply`, which skips any line whose source no
longer equals the text the search listed (hash-check semantics: nothing is overwritten that changed
outside). Open buffers change in place (one undo step, unsaved); other files are written on disk.

## Behaviour
- `:grep [-C n] <pattern> [path-glob]` runs the user's ripgrep as a regex (smart case) with `-g glob`
  (`search.GlobSearcher`). Quote a pattern containing spaces. `-C` defaults to 2 (max 20).
- `o` in the workspace search results pane opens the shown hits as the same buffer.
- Layout: two `#` note lines, then per file `path:` and rows `  12: matching text` (`  11- context`),
  `  --` between non-adjacent groups. Matches are highlighted and context dimmed via the theme.
- Over `searchBufMaxMatches` (1000) matches the buffer is cut, with a `#` note and the status message.
- `enter` (normal mode) or `gd` on a row, or a `path:` header (line 1), opens the file on that line.
- `:w` compares every row with what was listed; a changed row (match or context) is an edit. Lines
  that are not listed rows (new, renumbered, under an unknown header) are ignored and counted.
  Deleting a row applies nothing. The card lists files and line counts; `y`/enter applies, `n`/esc
  cancels. The result says how many lines were applied and, naming the first, how many were skipped
  because the source changed. Applied rows become the new baseline.
- `:grep!` re-runs the last `:grep` (or runs new arguments) and replaces the buffer in the same tab; a
  plain `:grep` refuses while the buffer has unwritten edits.
- Missing ripgrep: the existing "search needs ripgrep — install it" message and install card.
- The buffer is never autosaved, swapped, or restored as a session tab.

## Acceptance
- Argument parsing: `TestParseGrep`. Layout, context merge, snapshot: `TestBuildSearchBufContextAndGroups`.
- Edit detection and ignored lines: `TestPlanSearchEdits`.
- `:grep` opens the buffer, passes glob and pattern: `TestGrepOpensBufferWithGlobAndContext`;
  rg flags: `TestRipgrepSearchGlobPassesGlob`.
- Empty and missing-ripgrep messages: `TestGrepNoMatchesAndMissingRipgrep`; no glob searcher: `TestGrepNeedsGlobSearcher`.
- Cap and note: `TestGrepCapsMatches`.
- enter / gd open the row: `TestSearchBufEnterAndGdOpenRow`.
- Card before any write, cancel, apply: `TestSearchBufWriteAsksThenApplies`.
- Changed source is skipped and reported: `TestSearchBufSkipsChangedSource`; open buffers edited in place:
  `TestSearchBufApplyEditsOpenBufferInPlace`.
- `:grep!` refresh and the dirty guard: `TestGrepBangRefreshesAndPlainGrepKeepsEdits`.
- `o` from the results pane: `TestResultsPaneOpensBuffer`; autosave skip: `TestSearchBufNeverSavesSourceDirectly`.
- Goldens: `editor_search_buffer_100x30`, `editor_search_apply_card_100x30`.

## Open
- `-C` is an argument, not a Settings row (a persistent default would need a settings key).
