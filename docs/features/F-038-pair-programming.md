# F-038: Pair programming in the editor

Status: complete (M23 part A) · Companion to: ADR-0023 (editor tools
seam), F-035 Part B (co-editing indicator), F-036 (dispatch).

## Summary

Pairing is a session between the human's buffer and one agent: the agent
can *see* the human's position and *suggest* changes the human reviews,
instead of pasting a code block blindly or editing unreviewed.

## Behaviour

- **Invite:** `:pair <agent>` (editor ex command) opens the chat sidebar on
  the agent's DM, posts a kickoff that tells the agent about the tools,
  and shows `◆ pairing: <agent>` on the buffer title. `:unpair` ends it.
  Unknown agent / no file / no crew each refuse with a named message.
- **See:** served tool `editor_context` (Read scope) returns the active
  file, 0-based cursor, mode, any visual selection, ±12 lines around the
  cursor, nearby LSP diagnostics and the pending-proposal count.
- **Suggest:** served tool `editor_propose_edit` (path, old, new, note)
  validates against the live buffer (old text must occur exactly once)
  and queues a proposal. It needs no approval-queue entry; the human's
  accept/reject is the gate. `editor_apply_edit` stays the direct,
  approval-gated path.
- **Review:** a proposal opens a diff overlay: `y` accept (one undo step
  through the live buffer), `n` reject (the agent is told in its DM),
  `A` accept all, `esc` decide later (`ctrl+y` reopens; the title shows
  `◆ N suggestion(s)`). A proposal whose target text moved on is dropped
  with a visible "stale" note instead of misapplying.
- **Ask:** `:ask <question>` sends the visual selection (kept when `:`
  leaves visual mode, like vim) or the cursor line inline to the partner.
- The editor chat sidebar now dispatches the turn on send; before this a
  typed @mention reached the bus but no agent ever answered.

## Acceptance

- [x] Context reports cursor/selection/lines; refuses with no buffer
- [x] Propose validates (missing, ambiguous, no-op, empty, duplicate)
- [x] Accept = one undo step; reject untouched; defer + reopen; stale
      dropped named; accept-all
- [x] `:pair`/`:ask`/`:unpair` flows and refusals; badge on title
- [x] tools route through `PairAPI`, refuse without it, don't enqueue
      approvals; `textbuf.Selection` / `:`-from-visual capture

## Deferred

- Agent cursor/selection marker and a follow mode.
- Multi-hunk proposals (one old/new per proposal today).
- Inline (in-buffer) diff rendering instead of the overlay.
