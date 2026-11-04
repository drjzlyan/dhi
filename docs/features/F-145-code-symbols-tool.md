# F-145 — `code_symbols`: a repo map from the language servers

Status: done · gap G-B3

Agents found code by grepping. The language servers the editor already runs know every symbol
and its file, but only hover/definition/references (at a known position) were exposed. No
embeddings; this is a repo-map-lite.

## Behaviour
- Tool `code_symbols` (read scope, in `BuiltinTools` and the served slugs), over the ADR-0023
  editor seam as LSP ops `symbols_workspace` / `symbols_document`.
  - `{"mode":"workspace","query":"Load","path":"<member>/<rel>"?}`: `workspace/symbol`. `path`
    picks the language; without it every running server is asked.
  - `{"mode":"document","path":"<member>/<rel>"}`: `textDocument/documentSymbol` outline,
    indented by nesting.
  - Definition/references stay as `lsp_definition` / `lsp_references`.
- Output is `kind name member/file:line`. Workspace hits outside every member (stdlib,
  dependencies) are dropped, so paths are always VPaths; `path` goes through the same jail as the
  other path tools.
- Capped at 200 with `(truncated: showing 200 of N symbols ...)`.
- No server running for the language: plain `no language server for Go (...)`, no fallback, no
  start, no install (ADR-0011/0044). Servers start only when the human opens a file of that language.
- F-122: symbol names come from repo files, so results are fenced `<untrusted_data
  source="symbols">`. Status answers carry no repo text and stay bare.

## Acceptance
- Client parses both workspace/symbol shapes (`TestWorkspaceSymbolsShapes`).
- Member paths, outside-member drop, cap, outline, no-server answers
  (`TestSymbolsWorkspaceMemberPathsAndDropsOutside`, `TestSymbolsWorkspaceCapsAt200`,
  `TestSymbolsDocumentOutline`, `TestSymbolsNoServerIsAPlainAnswer`).
- Tool routing, fence, defused tag, refusals (`TestCodeSymbolsServedAndRoutes`,
  `TestCodeSymbolsFenceDefusesInjectedTag`, `TestCodeSymbolsNoServerStaysBare`,
  `TestCodeSymbolsRefusals`); scope Read (`scopes_test.go`).
