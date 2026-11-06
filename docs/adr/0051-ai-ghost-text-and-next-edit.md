# ADR-0051: AI ghost text is on demand through the agent CLI; no keystroke-rate engine

Date: 2026-11-04 · Status: accepted · Companion to: ADR-0019 (CLI is the engine), ADR-0024 (no
direct-API engine), ADR-0031 (budgets), ADR-0044, ADR-0046
Serves: gap G-X2.

## Context
The editor (`internal/tui/surfaces/editor`) has LSP completion and navigation but no inline AI
suggestions. Ghost text (as-you-type) and next-edit prediction need a completion every few hundred
milliseconds. ADR-0019 verified that none of the six CLIs has a raw-completion mode: each call starts
the CLI's whole agent loop, which costs seconds of start-up and a full prompt's tokens. ADR-0024
removed the direct-API engine kind, which is the usual way editors get fast completions. So the
feature as VS Code or Cursor does it conflicts with two accepted ADRs.

## Options
1. **Not pursued; rely on LSP.** Zero cost. Language servers that implement inline completion
   (LSP 3.18 `textDocument/inlineCompletion`) can feed ghost text without any model decision, but few
   servers do and none for general prose or boilerplate.
2. **On-demand suggestion through the file's agent CLI.** A chord (not a keystroke) asks the user's
   chosen agent for a short continuation at the cursor; the result shows as ghost text, Tab
   accepts. Latency is seconds and shown as a spinner; each call is metered by the budget ledger.
   No ADR-0019/0024 conflict.
3. **A local completion model on the user's machine** (e.g. an Ollama-style server found on PATH, per
   ADR-0044). Fast and free of vendor spend, but it is a direct-API engine for completions, so it
   needs ADR-0024 reopened for that one narrow use, plus an HTTP client and model management.

## Recommendation (the user decides): options 1 + 2 now, option 3 only if wanted later
- Show ghost text from an LSP inline completion when the server provides it; otherwise nothing
  appears. This is passive, free and silent only in the sense of "no suggestion", not a downgrade.
- Add an explicit `:suggest` / chord that runs one short, read-only agent call (no write scope, no
  tool loop, small context window: file excerpt plus cursor) on the user's default engine. Never
  auto-triggered; Settings may enable an idle-pause trigger later, labeled with its cost.
- Next-edit prediction is out of scope: it needs edit history modelling and keystroke-rate latency.
- Option 3 stays a separate future ADR that amends ADR-0024; this ADR does not pre-approve it.

## Consequences
- The feature is honest about latency and cost instead of imitating a product it cannot match.
- Spend appears in the ledger as its own line so a user can see what suggestions cost.
- Editor needs a ghost-text render layer in any case (theme-owned style, goldens).

## If accepted
1. `internal/langserver` and `internal/lsp`: negotiate and request inline completion.
2. `internal/tui/surfaces/editor`: ghost-text layer, accept/dismiss keys; `internal/tui/theme`: style.
3. `internal/agentkit/runtime`: a one-shot read-only suggest call with a bounded prompt; fence
   file content (ADR-0046) and meter it in `internal/agentkit/budget`.
4. `internal/settings`: opt-in and trigger mode; `internal/doctor`: row for engine and LSP support.
5. Regenerate editor goldens; spec F-TBD (next free number).

## Decision (2026-11-05, user)
Options 1 and 2: LSP inline completion first (ghost text when the server offers it), plus an
explicit on-demand agent suggestion (`:suggest` / chord), read-only, bounded, metered. No local
model (option 3) and no next-edit prediction.
