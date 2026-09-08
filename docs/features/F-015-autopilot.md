# F-015: Autopilots — scheduled agent work, in DHI flavor

Status: in progress (M8 P3) · Milestone: M8 · Depends on: F-013
Inspired by: Multica's Autopilots ("run standups, audits, and reports
on a cron — nobody to remind").

## Summary

DHI is a foreground TUI: no resident daemon, no system cron
(ADR-0004/0005 spirit — one binary, user-driven). The DHI flavor of
autopilots is therefore **schedule declarations evaluated while DHI
is open**: due-on-launch catch-up plus in-session ticks for
interval schedules. An autopilot is one named, scheduled engagement
of a rostered agent (either runtime kind) with a fixed prompt —
results land exactly where an @-mention would: the agent's channel
thread, the same run records, the same timeout/retry policy, the same
review gate.

## Part A — store (`internal/autopilot`)

- One card per autopilot under reserved `.dhi/autopilots/<slug>.toml`
  (reserved in the workspace schema, patterned on tasks/ideation):
  ```toml
  schema = 1
  name = "Morning standup"
  agent = "scout"            # roster id; dangling ref = warn + refuse at run
  prompt = "Summarize yesterday's runs and today's open tasks."
  schedule = "daily 09:00"   # strict: "interval <dur>" | "daily HH:MM" | "weekly <dow> HH:MM"
  enabled = true
  last_run = 2026-09-08T09:00:00Z   # runtime-maintained
  ```
  Strict decode; unknown keys refuse (ADR-0011); malformed cards are
  skipped with a warning and fail their doctor row (consistent with
  tasks/sessions).
- `Store`: `Open(ws)`, `List()`, `Due(now) []Card`, `MarkRan(slug, ts)`,
  `Subscribe` pings on change — the tasks/ideation pattern verbatim.
- **Due semantics** (pure function, table-tested):
  - `interval <dur>`: due when `now - last_run >= dur` (never ran ⇒
    due at first evaluation).
  - `daily HH:MM` / `weekly <dow> HH:MM`: due when the schedule
    instant is ≤ now and after `last_run`.
  - **Missed-run rule:** when DHI was closed over the schedule
    instant, the catch-up runs ONCE at launch with a
    "missed (last run …)" annotation in the post — never a backfill
    of every missed instant.

## Part B — execution

- **Launch catch-up:** at boot (after the gate releases), every due
  + enabled autopilot executes once, in slug order, through the
  ordinary turn seam (trigger kind `autopilot`, posted to the
  agent's DM channel so the transcript lives in the normal place);
  each `MarkRan` persists immediately.
- **In-session:** interval schedules arm a message-driven tick chain
  (the F-012 pattern: explicit `tea.Tick` re-arms, deterministic in
  tests); daily/weekly are re-evaluated on launch and on local date
  change while open. A paused (`enabled=false`) autopilot never arms
  a tick.
- Runs obey the agent's F-013 timeout/retry policy; failures post to
  the thread and surface in the inbox (F-016). An autopilot whose
  agent is missing/dangling refuses with the named fix — it does not
  silently pick another agent.

## Part C — UI (Workspace view)

- New AUTOPILOTS section (eighth pane of the `[`/`]` switcher):
  rail row = `name  agent  schedule  next-due  last-result`; keys:
  `n` new (form: name, agent from roster, prompt, schedule,
  enabled), `e` toggle arm/pause (flips `enabled`), `r` run now
  (posts to the thread immediately), `x` remove-with-confirm, `o`
  open the last transcript (F-014 replay). Empty state:
  "no autopilots — `n` to schedule one".
- New card writes through the store (persist-before-visibility, as
  tasks/ideation do).

### Acceptance criteria

- Schedule parser: table tests over valid (`interval 30m`, `daily
  09:00`, `weekly mon 08:30`) and invalid (`daily 25:00`,
  `weekly xmas`, bare `30m`) — invalid refuses with the value named.
- Due computation: table tests — first run (never ran), interval
  elapsed/not, daily across midnight, weekly dow wrap, last_run
  after the instant (not due), missed-multiple-instants ⇒ exactly
  one catch-up.
- Launch catch-up: with a mock turn handler, exactly the due set
  executes in slug order, each marked ran once; a paused card is
  skipped; a dangling agent refuses with the named fix and is NOT
  marked ran.
- In-session interval: explicit tick messages fire the run at the
  right boundary (deterministic; injected now/clock); pausing stops
  the tick chain (no re-arm).
- Store: strict decode (unknown key refused), malformed card ⇒ warn
  + doctor Fail naming the file; `MarkRan` round-trips through TOML.
- UI: goldens for the section (list, empty, next-due formatting);
  key handling per row (run-now posts via the mock handler).
- `make verify` green.

## Deferred

- Cron expressions (the three schedule shapes cover the documented
  Multica use cases — standups, audits, reports — without a parser).
- Resident mode (`dhi --serve`) so schedules fire while the TUI is
  closed: a product decision with packaging implications, not an
  implementation gap.
- Per-autopilot task-card creation (results currently ride the agent
  channel thread; one `tasks.Add` per run is a two-line follow-up if
  the thread becomes noisy).
