# ADR-0023: Editor/LSP tool seam — the app routes, the runtime asks

Date: 2026-09-26 · Status: accepted · Companion to: ADR-0019 (DHI owns
the loop), ADR-0018 (in-process serving), ADR-0014 (workspace IA), F-030
(the IDE tool surface). Serves: F-030 P1.

## Context

F-030 gives agents the editor and LSP as tools (`editor_open`,
`editor_reveal`, `editor_apply_edit`, `lsp_hover`, `lsp_definition`,
`lsp_references`, `lsp_rename`, `lsp_code_action`). But `dhitools` runs
inside the **runtime**, which holds no reference to the **editor
surface** that owns buffers, undo groups, and the LSP client. Reaching
around the app into a surface would break the single-owner invariant the
whole architecture rests on (surfaces are routed by the app; seams are
injected, never reached around — F-021/F-022).

The tool call itself runs on the MCP server goroutine and may block
(approvals already do), while the TUI is single-threaded. So the seam
must be an async request/response the app services on its own loop.

## Decision

1. **The runtime holds an app-owned `EditorAPI` seam.** A narrow
   interface (open, reveal, apply-edit, and one LSP verb entry point) is
   injected into `runtime.Config` at boot from `cmd/dhi`. It is the only
   door from the runtime to editor/LSP state.
2. **The app is the router.** `cmd/dhi` wires the seam to the app, which
   forwards each request into the Bubble Tea loop (a message, not a
   direct call) and replies on a channel. This preserves DBH's rule that
   the app owns routing and surfaces own their state (ADR-0014/F-021).
3. **Requests are blocking with context.** A tool call sends a request
   and waits for the reply or `ctx` cancellation (turn timeout). A nil
   seam (no workspace/editor, headless tests) makes every editor/LSP
   tool **refuse by name** — never a silent no-op (ADR-0011).
4. **Edits use the existing WorkspaceEdit path.** `editor_apply_edit`
   routes through the same bottom-up, one-undo-group application the
   human's LSP rename/code-action uses — agents get no privileged write
   path. LSP verbs reuse the editor's existing client and its guard on
   client absence.
5. **The seam carries VPaths, not absolute paths.** The editor resolves
   them through the workspace jail exactly as it does for human actions;
   the runtime never passes raw filesystem paths across the boundary.

## Consequences

- `runtime.Config` gains one seam; `dhitools` gains an
  `editor`/`lsp` tool group that refuses cleanly when the seam is nil.
- The app gains a request/response handler and one new message type; the
  editor surface exposes a small internal API the handler calls (not
  exported to the runtime).
- Headless and test paths (no TUI) exercise refusals, keeping the
  contract honest; live editor/LSP behavior is covered by surface tests.
- This is the last architectural piece of F-030 P1; once it lands the
  remaining deferrals are `run`'s per-agent command list (M16 workflows)
  and `git_push` (dropped in favor of the PR step).
