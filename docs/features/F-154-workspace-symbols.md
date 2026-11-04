# F-154 — Workspace symbols picker

Status: done · gap G-E11

`:sym` only lists the active file's outline. Finding a type or function elsewhere in the
workspace meant a text search. `lsp.Client.WorkspaceSymbols` (F-145) already serves the agent.

## Behaviour
- `:wsym [query]` (palette: "Go to symbol in workspace") opens a picker over `workspace/symbol`
  from every running language server. Nothing is started or installed.
- Typing re-queries: each key starts a new sequence number, re-ranks the rows already shown
  with `internal/fuzzy`, and schedules the query after a 150 ms debounce. It runs off the UI
  loop; a reply or tick for an older sequence is dropped.
- Only symbols inside a workspace member are listed (servers also index stdlib and deps).
- enter opens the file at the symbol; esc cancels. Up/down, ctrl+n/ctrl+k move.
- No running server: `wsym: no language server is running (...)`. Untrusted folder (F-148):
  `restricted: trust this folder to search workspace symbols (:trust)`.
- No dedicated key: ctrl+t is the drawer, and no free key is natural in nav mode; use `:wsym`
  or the palette.

## Acceptance
- Rows show `Container.Name`, kind, `member/path:line`; outside-member hits are dropped; enter
  opens the file at the line (`TestWsymPickerListsMembersAndOpensFile`).
- Each key bumps the sequence, re-ranks locally, and only the latest tick queries
  (`TestWsymTypingRequeriesAndRanksLocally`); late replies are ignored (`TestWsymDropsStaleReplies`).
- Named refusals (`TestWsymNoServerNamesTheReason`, `TestWsymRestrictedNamesTrust`).
- Palette entry opens the picker (`TestWsymPaletteEntry`).
