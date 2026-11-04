# F-169 — LSP inlay hints

Status: done · gap G-E16

Language servers can label code inline: the inferred type after a variable, a parameter name before
an argument (`textDocument/inlayHint`, LSP 3.17). The editor drew none of it.

## Decision
Hints are virtual cells. They take room on screen but are not text, so the buffer, the cursor, motions,
selections and soft wrap (F-136) keep addressing runes only. Off by default (`editor.inlay_hints`).

## Behaviour
- Setting `editor.inlay_hints` (Settings row "inlay hints"); `:set inlayhints` / `:set noinlayhints`
  override it for the session, like `wrap` and `blame`.
- Asked only when the setting is on and the server advertises `inlayHintProvider` (ADR-0011: a server
  that reports nothing is not asked). `:set inlayhints` names a server that cannot provide them.
- The visible window plus a screen of margin is requested after 250 ms of stillness (edits and scrolling
  restart it), off the UI goroutine, after the server has the current text. An answer for an older
  request is dropped by generation; hints are cached per buffer version. After an edit the cursor
  line's hints hide at once and the other lines' stay (when the line count is unchanged) until the new
  answer lands. `workspace/inlayHint/refresh` drops the cache.
- Drawn with the dim text style at the position, padding honoured; label parts are joined.
- The cursor block sits after the hints at its column; horizontal scroll, the completion popup anchor and
  wrapped rows count hint width. A click on a hint lands on the rune it precedes.
- gopls emits no hints until configured: while the setting is on the client sends gopls's standard hints
  (`assignVariableTypes`, `compositeLiteralFields`, `functionTypeParameters`, `parameterNames`,
  `rangeVariableTypes`) with `workspace/didChangeConfiguration` and answers `workspace/configuration`;
  turning the setting off sends them all false. Other servers use their own defaults.

## Acceptance
- Hints render inline, buffer unchanged (`TestInlayHintsRenderInlineWithoutChangingTheBuffer`,
  golden `editor_inlay_100x30`).
- Off by default and never requested while off or without server support
  (`TestInlayHintsDefaultOffAndNotRequested`, `TestInlayHintsNotAskedWithoutServerSupport`,
  `TestEditorInlayHintsLayer`, `TestInlayHintsToggle`); `:set` toggles (`TestInlaySetCommandTogglesForTheSession`).
- Cursor, selection and clicks skip hints (`TestInlayCursorAndClicksSkipHints`,
  `TestInlaySelectionIgnoresHints`, `TestDevirtCell`); wrap fits rows and maps clicks
  (`TestInlayWrapRowsFitAndClickMaps`, `TestWrapSegmentsHintedMatchesPlainWithoutHints`).
- Stale answers dropped, edits refetch, only the window is requested (`TestInlayStaleAnswerDropped`,
  `TestInlayEditHidesCursorLineHintsThenRefreshes`, `TestInlayRequestsOnlyTheWindow`).
- Client: `TestCanInlayHintsNeedsTheCapability`, `TestInlayHintsDecodeStringAndPartLabels`,
  `TestSetInlayHintsConfiguresGopls`.
- Live: `TestLiveGopls/inlay_hints` (`DHI_LIVE_LSP=1`) against real gopls.
