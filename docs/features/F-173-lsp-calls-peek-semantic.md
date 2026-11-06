# F-173 — Call hierarchy, peek, semantic tokens

Status: done · gap G-E22

Three language-server features the editor lacked: who calls (or is called by) a function, a look at a
definition without leaving the buffer, and server-computed colours for names the lexer cannot tell apart.

## Decision
- `gp` is free: vim's `gp` (put and move after) is not implemented in `textbuf`, and the surface swallows
  unknown `g` chords. If it is ever added, peek moves; `:peek` stays.
- gopls ships semantic tokens off and only advertises them when started with
  `initializationOptions {"semanticTokens": true}`. `lsp.StartProcess` sends that for gopls (no other server);
  nothing is requested unless `editor.semantic_tokens` is on.
- Client capabilities now announce `callHierarchy` and `semanticTokens` (full, relative); nothing else new.

## Behaviour
- `:calls` (incoming) / `:calls out`: a tree for the function under the cursor. right/l opens a node one level
  deeper (asked of the server then), left/h closes it or steps to the parent, enter jumps, esc closes.
  A server without `callHierarchyProvider` is refused by name.
- `gp` / `:peek` / `:peek refs`: a 10-line popup under the cursor showing the definition (or references) with the
  target line marked. esc closes, enter jumps, up/down step through several locations, any other key closes it
  and is processed (as hover does).
- `editor.semantic_tokens` (Settings, default off; `:set semantic|nosemantic` for the session): tokens from
  `textDocument/semanticTokens/full`, decoded with the server's legend, are layered over the syntax colours for
  namespace, type-like, function/method, parameter and readonly variable. Requested debounced (300 ms) after
  edits; a line edited since the answer keeps its syntax colours until the next one.

## Acceptance
- Decode, capability gates, call items round-trip the server's opaque `data`
  (`internal/lsp/hierarchy_test.go`).
- Incoming tree expands and jumps; outgoing; unsupported server refused
  (`TestCallsIncomingTreeExpandsAndJumps`, `TestCallsOutgoing`, `TestCallsNeedServerSupport`).
- Peek definition leaves the buffer untouched, esc/enter, refs stepping, empty answer named
  (`TestPeekDefinitionStaysInBuffer`, `TestPeekReferencesStepThroughLocations`, `TestPeekWithoutResultSaysSo`).
- Semantic overlay on, off by default, stale-line guard, refresh after an edit, multibyte/tab columns
  (`TestSemanticTokensLayerOverSyntax`, `TestSemanticTokensRefreshAfterEdit`, `TestSemanticLineMultibyteAndTabs`,
  `TestSemanticStyleMapping`); settings (`TestEditorSemanticTokensLayer`, `TestSemanticTokensToggle`).
- Real gopls: `TestLiveGopls/call hierarchy`, `TestLiveGopls/semantic tokens` (`DHI_LIVE_LSP=1`).
