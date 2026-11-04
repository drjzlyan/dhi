# F-128 — LSP signature help in the editor

Status: done · gap G-E1

Typing a call gave no hint of the function's parameters. The editor now asks the language server
(`textDocument/signatureHelp`) and floats the active signature under the cursor.

## Behaviour
- In insert mode, typing a character the server advertises (`signatureHelpProvider`
  `triggerCharacters` and `retriggerCharacters`, usually `(` and `,`) while inside an unclosed `(`
  requests signature help. A server that reports capabilities without `signatureHelpProvider` is
  never asked; one that reports none at all gets `(` and `,` (as with `CanFormat`).
- The popup shows the active signature (the server's `activeSignature`), the active parameter in
  the brand colour (string or `[start,end]` UTF-16 label forms; the signature's own
  `activeParameter` beats the response's), and the first line of its documentation.
- While open, `,` (trigger) and backspace refresh it; `)` refreshes it only if the cursor is still
  inside an enclosing call, otherwise it closes. It also closes on `esc` or any exit from insert
  mode, and when the cursor leaves the call. "In a call" is judged on the cursor line only.
- Answers are tagged with a request generation: one that lands after the popup was dismissed or
  superseded is dropped. A null/empty answer or an error shows nothing.
- Completion, code actions and hover take the popup slot ahead of it.

## Acceptance
- `TestSignatureHelpShapes`, `TestSignatureTriggers` (`internal/lsp`): result shapes, label
  offsets, active signature, null/empty, trigger capabilities.
- `TestSignatureHelpFollowsTheCall`, `TestSignatureHelpEscDismisses`,
  `TestSignatureHelpLeavingTheCallDismisses`, `TestSignatureHelpStaleAnswerIsDropped`,
  `TestSignatureHelpNeedsInsertMode`, `TestOpenParensBefore` (editor, against the fake server).

## Not covered
- Multi-line calls (the paren scan stays on the cursor line); no key to re-open the popup by hand.
