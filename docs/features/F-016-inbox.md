# F-016: Inbox — one place for everything that needs you

Status: in progress (M8 P4) · Milestone: M8 · Depends on: F-013/F-014
Inspired by: Multica's Inbox ("get pinged when an agent needs a call,
not for every step").

## Summary

DHI already produces every "needs a human" signal — pending tool
approvals, @-mentions, failed/timed-out runs, tasks sitting in
in-review — but they live in four different panes and the human has
to remember to look. The Inbox is a pure aggregation surface: no new
persisted state, no new semantics, one rail listing each open item
with a key that jumps to the pane that owns it. When an item resolves
in its home surface, it leaves the inbox; the inbox never resolves
anything itself (approvals are still answered in the chat sidebar,
reviews still decided in Reviewer).

## Part A — sources (each is an existing seam, read-only)

| kind | source seam | row text | jump |
|---|---|---|---|
| `approval` | `tools.Approvals.List()` | `approve  scout: write .dhi/tasks/x/y.md` | Editor chat approvals (opens the sidebar approval) |
| `mention` | bus: a message @-ing "you" with no later message from "you" in its thread | `#general  scout: "needs a decision on…"` | CHANNELS, thread opened at the mention |
| `run_failed` | F-014 run records: status failed/timed_out on a task not done | `run failed  fix-login (codex) — timeout after 10m` | run replay pane (F-014) |
| `in_review` | tasks: status `in-review` | `in review  fix-login — scout, 2 runs · $0.42` | Reviewer, that review/task |

- **Mention rule detail:** "no later message from you" is computed
  from the bus thread (you are a bus participant, "you" author id);
  a mention replied to is no longer in the inbox. No read-marks are
  invented for M8 — this is the M4-deferred unread marker, scoped to
  exactly the attention cases.
- Aggregation is a pure function `Inbox(approvals, bus, tasks, runs)
  []Item` — deterministic ordering: severity (approval > run_failed >
  in_review > mention), then oldest-first; stable, table-tested.
- No state writes: the inbox cannot mark-read, resolve, or delete.
  (A dismissed mention would need a read-mark model; that is the
  deferred "true unread" follow-up, not M8.)

## Part B — UI (Workspace view)

- New INBOX section (ninth pane of the `[`/`]` switcher):
  - Rail: one line per item, kind glyph (approval = warning diamond,
    run_failed = cross, in_review = check, mention = `@`) + the row
    text from Part A, word-wrapped at the rail width.
  - `enter`/`o` jumps to the owning surface (the jump column above);
    no other keys — the inbox is a launcher, not an editor.
  - Section rail count = item count; the statusline gets a
    `!N` segment only while N > 0 (cleared the frame after
    resolution), styled via theme (no raw colors).
  - Empty state: "nothing needs attention".
- Jump wiring reuses the existing navigation seams (editor chat focus,
  channels thread open, reviewer review select, INSPECT replay open) —
  each is a narrow method on the owning surface, injected like the
  reviewer's `OpenInEditor` seam; a missing seam degrades the row to
  a non-jumping hint naming the surface (visible, never silent).

### Acceptance criteria

- Aggregation: one fixture per source produces exactly one row with
  the expected text/kind; mixed fixtures order by severity then age;
  resolving a source (approval resolved, mention replied to, run's
  task done, task left in-review) removes the row on the next
  aggregation (pure-function test, no state).
- Mention rule: a mention with a later "you" reply in-thread is
  excluded; an unreplied mention in a DM is included.
- Statusline: `!N` appears iff N > 0 (tests over the composed view);
  the segment uses a theme style (lint-clean).
- Jump: each kind routes to the right surface method (test doubles
  observe the call); a missing seam renders the named hint and does
  not crash.
- Goldens: populated inbox (one of each kind), empty state, narrow
  width (wrap).
- `make verify` green.

## Deferred

- True unread/read-mark model (the M4-deferred item) — the mention
  rule is a deliberate subset until that lands.
- Inbox notifications from chat channels (Slack/Lark/Telegram): the
  integration DHI has declined to build; the surface is ready for a
  local trigger (autopilot completions, doctor regressions) first.
- Per-item snooze ("remind me later") — needs a persisted
  snooze state, i.e. the inbox stops being pure; revisit with the
  read-mark model.
