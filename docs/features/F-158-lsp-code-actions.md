# F-158 — LSP code actions and quick fix

Status: done · gap G-E12

`ga` already opened a popup of the server's actions for the cursor line, but it listed them in server
order, never resolved lazy actions (no edit, `data` only), applied an action's edit and dropped its
command, gave no hint that a fix existed, and had no organize-imports command.

## Behaviour
- `ga` (normal: the cursor line; visual: the selection) lists the actions with quick fixes first
  (preferred first), then refactors, then source actions; disabled actions are dropped. Only the
  diagnostics on the requested lines are sent as context.
- A marker (`theme.GlyphBulb`) joins the diagnostic chip in the buffer title when the cursor line has
  diagnostics and a `quickfix`-only request returns an action. One probe per cursor line, only against a
  server that is already running.
- Picking an action: when it has neither edit nor command and the server advertises
  `codeActionProvider.resolveProvider`, `codeAction/resolve` runs first. The WorkspaceEdit then applies
  (one undo group per file; closed files are written), then the command runs through
  `workspace/executeCommand`; edits the server pushes via `workspace/applyEdit` land the same way.
  Bare Command results and file create/rename/delete operations are decoded safely (the latter skipped).
- `:organize` asks for `source.organizeImports` over the whole file and applies the first action; with
  none, the command line says the server offers no organize-imports. Also in the palette.
- Restricted mode (F-148): servers do not start in an untrusted folder; `ga` names the gate and
  `:organize` answers `restricted: trust this folder to organize imports (:trust)`.

## Acceptance
- Ordering and disabled filtering: `TestRankActionsQuickfixFirstDropsDisabled`, `TestCodeActionPopupOrdersQuickfixFirst`.
- Marker: `TestLightbulbShowsOnQuickFixLine`, `TestLightbulbAbsentWithoutQuickFix`.
- Visual selection: `TestVisualGAOpensActions`.
- Resolve and single undo: `TestCodeActionResolveFillsEdit`, `TestCodeActionOnlyAndResolve`.
- Command plus server applyEdit: `TestCodeActionCommandAppliesServerEdit`, `TestBareCommandActionDecodes`.
- Organize: `TestOrganizeImports`, `TestOrganizeImportsNoneOffered`, `TestOrganizeRestrictedUntilTrusted`.
