# F-017: True unread — read watermarks, snooze, indicators everywhere

Status: implemented (M9, 2026-09-09) · Milestone: M9 · Depends on: F-016
(inbox), M4-P3 (channels). Inspired by: Slack's read-watermark model;
closes the M4-deferred unread marker that F-016 only approximated.

## Summary

F-016's mention rule ("a message @-ing you with no later message from
you in its thread") is a stateless heuristic: it can't see that you
*viewed* a DM, can't dismiss a message without replying, and can't say
"remind me later". F-017 replaces it with a real read-mark model:

- **Slack-style read watermarks** — opening a channel/DM/thread (or
  posting in it) advances a persisted per-channel "read up to here"
  cursor. Unread = agent messages after the cursor.
- **Attention predicate** — an agent message needs you only when it is
  *addressed to the human*: any agent message in a DM (`dm:<agent>` is
  1:1) or a channel message mentioning `@you`. Agent-to-agent chatter
  (agent A @-ing agent B in a channel) is never an item.
- **Snooze** — "remind me later" on any inbox item: the item leaves the
  count and reappears at full severity when the snooze expires.
- **Everywhere** — unread counts surface on the workspace CHANNELS
  rail, the editor chat header, the INBOX pane, and the `!N`
  statusline (which now excludes snoozed items).

User decisions (2026-09-09): read semantics = Slack-style view
watermark; items = all unreplied agent messages addressed to me +
mentions, never agent-to-agent; scope = CHANNELS rail + editor chat +
INBOX; snooze included.

## Part A — store: `.dhi/unread.json`

One mutable, strict JSON file (the `marketplace.json` precedent — a
single per-user state file under `.dhi`, gitignored; distinct from the
append-only JSONL history and the TOML cards). Runtime state, NOT a
tracked contract: it is never committed, never synced, and a missing
file is the fresh-install state, not an error.

```json
{
  "schema": 1,
  "channels": {
    "#general": {"readUpToID": 42},
    "dm:scout": {"readUpToID": 7},
    "dm:scout#12": {"readUpToID": 15}
  },
  "snoozes": [
    {"channel": "dm:scout", "messageID": 12, "until": "2026-09-10T09:00:00Z"}
  ]
}
```

- `channels` keys: `#name` and `dm:<id>` for top-level watermarks;
  `<channel>#<threadID>` for opened-thread watermarks (Slack treats an
  opened thread as read separately from the channel).
- `readUpToID` is the largest bus message ID the human has seen for
  that scope. Watermarks are **monotonic**: `MarkRead` only advances,
  never rewinds (an older ID is a no-op — F-011 no silent surprises).
- `snoozes` is a flat list of active snoozes; expired entries are
  dropped on load (named in the load log, not an error).
- **Strict decode** (ADR-0011 / F-011): unknown keys refuse with the
  key named; unknown `schema` refuses with the value named; an invalid
  `until` (unparseable) refuses naming the offending snooze. A
  corrupted file blocks the unread features with a named doctor row —
  it never silently resets the user's read state (resetting = guessing
  what was read).
- **Fresh-install seeding**: a missing file does NOT flood the inbox
  with history. First open seeds every existing channel's watermark at
  its current top-level max ID (and no thread watermarks) — existing
  history is considered read, only *new* messages count. Idempotent,
  table-tested.
- Atomic write (temp + rename) under a mutex; `Subscribe()` change
  pump like the other stores (task/autopilot pattern) so surfaces
  re-render without polling.
- Pruning: on load, watermarks for channels no longer present on the
  bus are dropped (named, not an error).

## Part B — attention predicate + aggregation

Pure function, table-tested, in `internal/unread` (the store package
owns the predicate; `internal/inbox` consumes its output):

```
AddressedToHuman(bus.Message) bool
```

- `m.Author == "you"` → never (you are not unread to yourself).
- `dm:<agent>` channel → yes (1:1 by construction).
- `#channel` → only when `bus.Mentions(m.Text)` contains `"you"`.
- agent A mentioning agent B in a channel → never.

Unread items for a scope = messages in scope with `ID > readUpToID`,
addressed to the human, and not under an active snooze. Threads: a
top-level mention/DM item whose thread the human opened uses the
`<channel>#<threadID>` watermark instead of the channel one.

`inbox.Build` change: the F-016 `mention` kind becomes
`agent_message` (row text unchanged: `dm:scout  scout: "…"` /
`#general  scout: "@you …"`); severity order unchanged (approval >
run_failed > in_review > agent_message). Everything else about the
inbox (approval / run_failed / in_review sources, jumps, `!N`) stays.

**Snoozed items** are excluded from the `!N` count and from jump
candidates, but remain visible in INBOX dimmed with a
`snoozed until HH:MM` suffix (F-011: visible, never silent). When the
expiry passes they return to full severity on the next render.

## Part C — marking read (the Slack rule)

A scope is marked read when the human is demonstrably looking at it:

1. **Opening a channel** — switching the active channel in the
   workspace CHANNELS pane, or opening/focusing the editor chat on a
   channel (the transcript is visible the instant the channel is
   active, so active = read). Advances the top-level watermark to the
   current max ID.
2. **Opening a thread** — `t` in CHANNELS (and the inbox mention jump,
   which drills into the thread). Advances the thread watermark.
3. **Posting** — sending a message in a channel/DM marks it read
   (you were there to post).

`MarkRead` is a synchronous store call (small file); a failed write
surfaces as a named flash, never swallowed. The two surfaces share the
one store, so opening a DM in the editor chat clears its workspace
CHANNELS-rail count too — one read state, two views of it.

## Part D — UI

1. **Workspace CHANNELS rail**: each label with unread top-level items
   gets a `●N` suffix (theme danger, no raw colors); N omitted when 1
   (plain `●`). Active channel renders its marker too until the
   opening-mark applies.
2. **Editor chat header**: channel name + the same `●N` suffix when
   the active channel has unread.
3. **INBOX pane**:
   - `agent_message` rows as F-016 mention rows (same text, same
     jump-to-owner).
   - Snoozed rows dimmed with `snoozed until <HH:MM>` (or
     `<HH:MM Mon d>` when past midnight).
   - New key `z` = snooze the selected item: small form with presets
     (15m / 1h / 4h / tomorrow 09:00); `z` again (or `u`) on a
     snoozed row = unsnooze. Nothing else edits — the inbox stays a
     launcher.
   - While any snooze is active, an explicit 30s `tea.Tick` chain
     re-renders the surface (the F-012 message-driven pattern, the
     F-015 tick-chain guard) so expiries flip without interaction;
     tests drive it with explicit tick messages, no sleeps.
4. **Statusline `!N`**: unchanged mechanism (per-frame
   `AttentionCount`), now counting unsnoozed items only.

## Part E — doctor

`unread/store` row, wired into `Run()` after the `autopilots` row:

- malformed file / unknown key / bad schema → Fail naming the
  offending key/value (the features degrade to a visible
  "unread unavailable: <reason>" hint in the rail — never silent,
  never a silent reset).
- snooze referencing a channel absent from the bus → Warn naming the
  channel (dangling, the autopilot pattern).
- expired snooze entries found on disk → dropped on load (counted in
  the row, not an error).
- silent when the file is absent or empty (fresh install).

## Acceptance criteria

- Predicate: table tests — DM agent message → item; channel @you →
  item; channel agent→agent mention → none; own message mentioning an
  agent → none; agent message at/before the watermark → none;
  mentioned message in an opened-thread thread → none, in an
  unopened one → item.
- Store: strict decode refuses an unknown key / bad schema / bad
  `until` naming the value; `MarkRead` is monotonic (older ID no-op);
  missing file seeds from current max IDs (no history flood) and is
  idempotent; atomic write round-trips; dangling channels pruned on
  load.
- Read-on-open: switching a channel marks it read (next Build shows 0
  for it); opening a thread marks only the thread; posting marks the
  channel; editor-chat and workspace-rail counts clear in lockstep
  (shared store).
- Snooze: `z` form presets land a snooze (row dimmed, `!N` excludes
  it); expiry via explicit tick message flips it back at full
  severity; `u`/`z` unsnoozes immediately; expired-on-load entries
  dropped.
- Jump resolves: `enter` on an `agent_message` row opens the
  channel/thread, marks it read, and the row is gone on the next
  render (the F-016 contract, now via the watermark).
- Goldens: CHANNELS rail with `●`/`●N` markers; editor chat header
  badge; INBOX with a snoozed dimmed row; statusline `!N` excluding
  snoozed (app test).
- `make verify` green; gofmt clean.

## Deferred

- Snooze-expiry *push* (a bell/notification the moment a snooze fires)
  — M9 re-renders on the tick while the surface is open; real
  notification plumbing waits for a design.
- Per-message (sub-thread) granularity and partial-thread read
  (scrolling through an old thread marks from the top).
- Autopilot completions / doctor regressions as inbox items — the
  F-016 surface is ready for local triggers; this spec keeps the
  agent-message model as the only new source.
- ~~Multi-human read states~~ — **closed (not planned)**: DHI is
  single-human (`bus.Human`).
- "Mark everything in this channel read" as an explicit command
  (opening already does; a bulk all-channels reset is a candidate for
  settings).
