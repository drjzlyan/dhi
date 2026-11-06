# F-181 — Ghost text: LSP inline completion and on-demand `:suggest`

Status: done · gap G-X2

ADR-0051 (accepted): no keystroke-rate engine. Ghost text comes from the language server's inline
completion when it offers one, and from one explicit, read-only agent call when the user asks. No next-edit
prediction, no local model.

## Decision
Ghost text is a virtual cell run like an inlay hint (F-169): it takes room on screen but is not text, so the
cursor, clicks and soft wrap keep addressing buffer columns. It shows only in insert mode, with one cursor,
at the end of a line (a suggestion between the cursor and following text would sit under the cursor block).
The cursor block is the cell between the typed text and the ghost.

## Behaviour
- **LSP (passive).** Client advertises `textDocument.inlineCompletion`. When the server advertises
  `inlineCompletionProvider`, `textDocument/inlineCompletion` (trigger Automatic) is requested 250 ms after
  typing pauses, off the UI goroutine, after the server has the current text; an answer for an older request,
  buffer version or cursor is dropped. A proposal whose range ends at the cursor re-types the typed prefix:
  the ghost is what follows it, and a proposal that does not extend the typed text has no ghost. Snippet
  proposals show their expansion; accepting starts a snippet session (F-171). A server that does not
  advertise it is never asked (ADR-0011). gopls does not (recorded by the live handshake test), so there
  the ghost stays off and `:suggest` still works.
- **Setting** `editor.inline_completion` (Settings row "ghost text", default on, effective only when the
  server provides it); `:set ghost` / `:set noghost` override it for the session, like `wrap`.
- **Keys.** Insert-mode tab, first match wins: completion popup, snippet stop, ghost text, indent. Tab on
  ghost text inserts it as one undo step. esc drops it (and still leaves insert mode). Typing the
  suggestion's next character walks through it; any other edit drops it.
- **`:suggest`** (normal or insert mode) and the chord **alt+\\** (insert mode; free in the editor, the app and
  the terminal bindings; it is VS Code's "trigger inline suggestion" key). It runs ONE short read-only agent
  call on the default agent: `Runtime.Suggest` refuses by name for a CLI without a read-only mode
  (F-144 `ReadOnlyOK`) and when a budget is reached, never resumes a session, posts nothing to the chat, and
  gives the CLI no DHI tools. The prompt is bounded (3000 runes before the cursor, 1000 after) with the
  excerpt inside the `<untrusted_data>` fence and the cursor marked `<<CURSOR>>` (ADR-0046), asking for a
  short continuation only. A dimmed spinner shows after the cursor while it runs; esc (or typing) cancels it.
  The answer becomes ghost text if the text and cursor are unchanged; from normal mode it enters insert mode
  at the point `a` would. Empty answers and errors are named on the status line. Never auto-triggered.
- **Metering.** The run lands in the spend ledger (`.dhi/spend.jsonl`) with `kind: "suggest"`; it counts toward
  the agent and workspace daily budgets, and the agent's HQ record shows `suggest 7d $x · N calls` as its own
  line when there were any.
- Help overlay lists tab-on-ghost, `alt+\\ · :suggest` and `:set ghost`.

## Acceptance
- Server proposal shown, buffer untouched, tab accepts, one undo step
  (`TestGhostTextFromServerShowsAndTabAccepts`, golden `editor_ghost_100x30`); esc, typing past, re-request
  (`TestGhostTextEscDismissesAndNormalModeShowsNone`, `TestGhostTextTypingPast`,
  `TestGhostTextRequestedAgainAfterATypedEdit`).
- Setting off or no capability: never requested (`TestGhostTextOffNeverRequestsAndSetCommandTogglesTheSession`,
  `TestGhostTextWithoutCapabilityIsNeverRequested`); `TestEditorInlineCompletionLayer`,
  `TestInlineCompletionToggle`.
- Range/prefix, snippet, multi-line, mid-line, wrap, click (`TestGhostTextRangeRetypingThePrefix`,
  `TestGhostTextDroppedWhenItDoesNotExtendTheTypedWord`, `TestGhostTextSnippetAcceptStartsASession`,
  `TestGhostTextMultiLineInsertsEverything`, `TestGhostTextOnlyAtTheEndOfALine`, `TestGhostTextWithWrap`,
  `TestGhostTextClickOnItLandsAtTheLineEnd`).
- Precedence popup > snippet > ghost > indent (`TestGhostTabPrecedence`).
- LSP wire: `TestCanInlineCompletionNeedsTheCapability`, `TestInlineCompletionDecodesStringSnippetAndListShapes`,
  `TestDecodeInlineNullAndBareArray`; live gopls handshake logs that it does not advertise it.
- `:suggest`: spinner then ghost, golden `editor_suggest_pending_100x30`, normal-mode form, excerpt, never
  automatic, esc cancels, stale answer dropped, errors/empty/no agent/one at a time, independent of the LSP
  setting (`TestSuggest*` in `ghost_test.go`).
- Runtime: one read-only spawn, fenced and bounded prompt, ledger line `suggest`, refusal by name for a CLI
  without read-only, budget refusal (`TestSuggestRunsOneReadOnlySpawnWithAFencedExcerpt`,
  `TestSuggestPromptIsBounded`, `TestSuggestFencesInjectedTagsInTheExcerpt`,
  `TestSuggestRefusesACLIWithoutAReadOnlyMode`, `TestSuggestRefusesWhenTheBudgetIsReached`,
  `TestCleanSuggestion`); HQ line (`TestPerfShowsSuggestSpendAsItsOwnLine`).
