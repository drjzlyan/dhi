# F-175 — Breadcrumbs and sticky scroll

Status: done · gap G-E15

The buffer title named only the file, and a long function scrolled its own signature out of sight.
Both are answered by the language server's outline (`textDocument/documentSymbol`, the data behind
`:sym`).

## Decision
Both read one per-file outline cache and nothing else. Without a server, or with one that does not
advertise `documentSymbolProvider`, nothing is guessed (ADR-0011): breadcrumbs show the path alone and
nothing is pinned. No indentation fallback. Breadcrumbs default on (they cost one cached request and
a few title cells); sticky scroll defaults off (it covers text rows).

## Behaviour
- Settings `editor.breadcrumbs` (on) and `editor.sticky_scroll` (off), rows "breadcrumbs" and "sticky
  scroll"; `:set [no]breadcrumbs` and `:set [no]sticky` override for the session.
- Breadcrumbs: the title reads `member › dir › file › Symbol › Method` (path from the tab's label,
  symbols are the outline entries whose extent holds the cursor line, outermost first). It is cut from
  the left to fit the panel (`… › `), never below the last segment, which is clipped if it alone is too
  long. Works in every pane of a split.
- The outline is requested once the buffer has sat still for 300 ms (edits restart it), off the UI
  goroutine, after the server has the current text; an older answer is dropped; the last good outline
  is kept on an error (reported once). Only asked while a setting that reads it is on.
- Sticky scroll: the header line (the symbol's name line) of each enclosing symbol that is scrolled
  above the viewport is pinned over the first rows, on the inset shade (`theme.StickyRow`), up to 3 (the
  innermost when more), outermost first, never covering the cursor's row. Not drawn while an edit has
  moved the outline (line count changed) until the new answer lands. A click on a pinned row jumps to
  that symbol's line. Works per pane, with soft wrap (F-136) and inlay hints (F-169).
- Not done: a key that opens `:sym` focused at the current symbol (optional in the gap).

## Acceptance
- Crumbs follow the cursor, path alone without a server or capability, off when disabled
  (`TestBreadcrumbsShowPathAndSymbolsAtTheCursor`, `TestBreadcrumbsWithoutAServerShowThePathOnly`,
  `TestOutlineNotAskedWithoutCapability`, `TestBreadcrumbsOffShowsJustTheFileName`,
  `TestOutlineNotAskedWhileBothAreOff`); left truncation (`TestCrumbsTruncateFromTheLeft`,
  `TestBreadcrumbsFitTheTitleOfANarrowPane`); nesting (`TestEnclosingChain`); debounced refresh
  (`TestOutlineRefreshesAfterAnEditDebounced`).
- Sticky: pins, only above the viewport, max 3 innermost, never over the cursor, click jumps
  (`TestStickyScrollPinsTheEnclosingHeaders`, `TestStickyScrollNeedsTheHeaderAboveTheViewport`,
  `TestStickyScrollPinsAtMostThreeInnermost`, `TestStickyScrollNeverCoversTheCursor`,
  `TestStickyClickJumpsToTheHeader`, `TestStickyScrollOffPinsNothing`); wrap, hints, panes
  (`TestStickyScrollWithWrap`, `TestStickyScrollWithInlayHints`, `TestStickyScrollInSplitPanes`).
- Settings and `:set` (`TestEditorBreadcrumbsAndStickyLayer`, `TestBreadcrumbsAndStickyToggle`,
  `TestSetCommandsToggleForTheSession`, `TestStickyNoteNamesAMissingServer`); golden
  `editor_breadcrumbs_sticky_100x20`.
- Live: `TestLiveGopls/breadcrumbs_and_sticky` (`DHI_LIVE_LSP=1`) against real gopls.
