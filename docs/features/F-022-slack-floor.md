# F-022: The Slack floor — CHANNELS rebuilt

Status: implemented (M11 P4, 2026-09-10)
(channels), F-017 (unread watermarks), F-016 (inbox jumps). Companion
to ADR-0014, F-021.

## Summary

CHANNELS becomes a Slack-like screen inside the workspace: a left
vertical channel rail, a center transcript with the composer, and a
right context pane that shows threads beside the transcript (not
instead of it) or an agent profile. The chatPane's bus, unread, and
mention-routing contracts carry over unchanged; the layout and thread
UX change.

## Layout (wide)

```
│ rail 26 │ # channels      │ transcript + composer │ context pane │
```

- **Left rail** (vertical, replaces the horizontal text rail):
  `CHANNELS` group (#general, then #<team> per org team) and
  `DIRECT MESSAGES` group (one per rostered agent). Active channel
  highlighted (theme selection bg); unread `●`/`●N` markers as today;
  group headers dim. Rail nav: `↑/k`, `↓/j` move the channel
  selection; `enter` opens; the current transcript keys stay.
- **Center**: channel header, transcript (word-wrap, author colors,
  `↳` reply tags, message cursor `j/k`), composer line at the bottom
  (`i` focus, `esc` blur, `enter` post as `you`, @mentions route
  through the runtime — all unchanged).
- **Right context pane** (context modes):
  1. `—` (closed, transcript gets the full width minus a gutter).
  2. **Thread** (`t` on a message; `c`/`esc` closes): the root
     message + its replies, composer inside the pane (posts to the
     thread). The transcript stays visible beside it.
  3. **Profile** (`v` on the rail selection or on an agent-authored
     message): the `internal/agentkit/profile` aggregation for that
     agent — identity, current work, teams, activity, memory, runs —
     the former INSPECT profile view re-hosted; `esc` closes.

## Layout (narrow, < ~100 cols)

- Context pane overlays the transcript (today's drill-down behavior):
  `t` thread / `v` profile replace the transcript column; `c`/`esc`
  returns. The rail stays.

## Read rules (unchanged, F-017)

- Opening a channel / thread / posting marks read (watermarks);
  `openAt(channel, thread, msgID)` — the inbox jump seam — selects
  the channel, opens the thread in the context pane, positions the
  cursor, and marks read.
- The editor chat sidebar shares the same store and is unchanged.

## Keys (delta from today)

| key | now |
|---|---|
| `↑/↓` or `j/k` in rail focus | move channel selection |
| `t` | open thread in the context pane (right on wide) |
| `c` / `esc` | close context pane (thread or profile) |
| `v` | agent profile in the context pane |
| `,` / `.` | prev/next channel (kept) |
| `i` / `enter` | composer focus (kept) |

## Acceptance criteria

- Rail renders groups in order with unread markers; selection
  survives roster/team rebuilds (rebuild contract kept).
- Thread: `t` opens beside the transcript; replies post to the right
  thread id; `esc`/`c` closes and the watermark was advanced on open.
- Profile: `v` renders the profile for the DM agent / message author;
  esc closes; missing sources degrade to named dim rows (profile's
  own rule).
- `openAt` (inbox mention jump) lands the thread in the context pane
  and marks it read — the inbox row clears next render.
- Unread markers clear on open in lockstep with the editor sidebar
  (shared store).
- Goldens: rail + transcript + thread pane; profile pane; narrow
  thread overlay.
- `make verify` green.
