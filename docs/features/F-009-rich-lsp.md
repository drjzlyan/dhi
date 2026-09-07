# F-009: Rich LSP features

Status: done (M7, 2026-09-02) · Milestone: M7 · Extends: [F-002](F-002-editor.md) §7

## Summary

The M2 LSP foundation (stdio client, didOpen/didChange, diagnostics,
ctrl+space completion) grows the features that make the editor feel like
an IDE for Go: **hover** documentation, **rename** across open buffers,
and **code actions** (quickfixes) off the diagnostics gutter. Everything
rides the existing hermetic gopls seam (no host fallback, ADR-0005); a
missing server degrades to today's behavior silently.

## Client additions (`internal/lsp`)

- **Types:** `Position`, `Range`, `TextEdit`, `Command`, `CodeAction`,
  `WorkspaceEdit` (both `changes` and `documentChanges` shapes).
- **Requests:** `textDocument/hover`, `textDocument/rename`,
  `textDocument/codeAction` (cursor-line diagnostics passed as context).
  Results are flattened for TUI consumption (hover markup → plain text).
- **Server→client `workspace/applyEdit`:** decoded, auto-answered
  (`applied: true`) and routed to the UI as an `EvApplyEdit` event; the
  reader loop never blocks on slow consumers.
- **Diagnostics clear fix (M2 gap):** `Event` carries the publishing
  path even for empty publishes, so a clean file actually clears its
  gutter instead of relying on the clear-all fallback.

## Editor surface keys (normal mode, buffer focus)

| Key | Action |
|---|---|
| `K` | hover popup for the identifier under the cursor |
| `gr` | rename identifier under cursor (input line → server WorkspaceEdit) |
| `ga` | code-action popup for diagnostics on the cursor line |

- `g` is a dead key in textbuf, so the surface owns the two-key
  sequences: `g` arms a pending prefix; an unrecognized second key
  cancels (never forwards, so no accidental `x` deletes).
- Hover popup closes on any cursor movement or `esc`; renders plain
  text, truncated to pane width.
- Rename reuses the git-commit input-line pattern: prompt shows
  `rename: <old> → `; `enter` dispatches `textDocument/rename` and
  applies the returned WorkspaceEdit; `esc` cancels.
- Code actions render in the completion popup's list machinery
  (`j`/`k` navigate, `enter` applies); an action carrying a
  WorkspaceEdit applies it, one carrying a command runs
  `workspace/executeCommand`.

## Edit application

- Edits target **open buffers only** (matched by absolute path per
  buffer). Each file's edits apply bottom-up so positions stay valid,
  wrapped in an undo group; the buffer is marked dirty, never
  auto-saved.
- Closed paths are skipped with a status note — opening arbitrary
  project files from a rename is deferred.
- After applying, the buffers re-sync to the server (existing
  `lspSync` path) so diagnostics refresh.

## Acceptance criteria

- Fake-server tests (mirroring the M2 completion harness) cover hover
  render, rename round-trip (input → WorkspaceEdit → buffer text),
  code-action list + apply, and `workspace/applyEdit` handling.
- An empty publish clears diagnostics for its path (gutter returns to
  plain); unrelated files keep theirs.
- No server / non-Go buffer: `K`/`gr`/`ga` are inert no-ops.
- `make verify` stays green; goldens regenerated only if popups change
  existing views (they render above the command line only).

## Deferred

- prepareRename range validation (client-side word extraction only),
  references/definition navigation, auto-open-and-apply for closed
  files, hover markdown styling.
