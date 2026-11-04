# F-132 — LSP `textDocument/didSave` and `textDocument/didClose`

Status: done · gap G-E2

Saving and closing files were silent to the language server. The editor now sends `textDocument/didSave` after save and `textDocument/didClose` when a buffer/tab is closed, mirroring how `didOpen`/`didChange` notify the server of document state.

## Behaviour

- **didSave:** When a buffer is saved (`:w` or `ctrl+s`), if the server advertises save capability and the client finds the buffer in an open tab, the editor sends `textDocument/didSave` with the file URI. If the server's capability includes `{save: {includeText: true}}`, the editor includes the full text in the notification.
- **didClose:** When a buffer/tab is closed (`:q`, `alt+w`, or via member removal), the editor sends `textDocument/didClose` with the file URI, once per open tab holding that path. The notification is only sent if the tab previously received a `didOpen` (i.e., the server was running when it opened).
- **Save ordering:** If format-on-save rewrites the buffer or unsent edits exist, `didChange` is sent first with the new content, then `didSave` follows.
- **Capability detection:** 
  - No capabilities reported (`nil`): send both notifications (benefit of the doubt for test servers).
  - `textDocumentSync` field omitted or does not contain `save`: don't send didSave.
  - `save: true` or `save: {}`: send didSave without text.
  - `save: {includeText: true}`: send didSave with text.
  - Per ADR-0011, omitted capabilities are not guessed.

## Acceptance

- `TestDidSaveCapabilities` (`internal/lsp`): server capability shapes for didSave.
- `TestDidSaveAndChange`, `TestDidSaveWithText`, `TestDidSaveWithoutText`, `TestDidCloseOpenedTab`, `TestDidCloseDontSendIfNeverOpened`, `TestDidSaveCapabilityOmitted` (editor, against fake LSP): notification order, text inclusion, URI correctness, and skipping unopened tabs.

## Not covered

- Save-as (`:w path`) when it changes `ed.Path()` but the URI was never opened. Handled by not sending didClose for this tab on close (only tabs that received didOpen are tracked).
