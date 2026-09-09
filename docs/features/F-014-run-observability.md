# F-014: Run observability — execution log + cost accounting

Status: implemented (M8 P2, 2026-09-09) · Milestone: M8 · Depends on: F-013
Inspired by: Multica's execution log ("replay every tool call, command,
and error, timestamped") and per-agent/per-issue token usage.

## Summary

A delegated run (F-013) leaves two artifacts: the transcript
(`.dhi/agents/<id>/runs/*.jsonl`) and the `[[run]]` record on the task
card. This feature makes them first-class, viewable surfaces: a
uniform run schema that CLI turns feed, cost/usage rollups per agent
and per task, and a run-replay pane in INSPECT. No new execution
machinery — this is aggregation + rendering over F-013's records.

## Part A — uniform run schema

- The `[[run]]` record (introduced by F-013) is THE run schema:
  `ts, runtime` (`cli:<name>`), `model`, `status`,
  `exit`, `duration`, `tokens_in`, `tokens_out`, `cost_usd`,
  `transcript`, `attempt`, plus `task` (the card slug, when the run is
  bound to one; otherwise the thread only).
- **CLI runs** fill it completely (cost from the terminal event;
  `cost_usd` 0 + a declared `cost: false` marker when the CLI reports
  none — never a guessed number). ADR-0013 removed the in-house
  engine, so there are no anthropic runs to reconcile; `runtime` is
  always `cli:<name>`.
- Rollups are pure math over records: per-agent totals (runs,
  succeeded/failed/timed_out, tokens, cost sum over costed runs only),
  per-task totals (same, for that card's runs). A run with
  `cost=false` is counted in runs/tokens-known columns but excluded
  from the cost sum; the UI marks the sum "partial" when any run in
  the set is cost-less.

## Part B — surfaces

- **INSPECT profile** (existing): the agent's identity block gains a
  RUNS subsection under activity — totals line (runs · ok/fail/timeout
  · tokens · cost or "cost partial"), then the last 5 runs, newest
  first: `ts  cli/model  status  duration  cost`.
- **Run replay pane:** `enter`/`e` on a run row opens the replay —
  the transcript jsonl rendered chronologically: one line per event
  (progress text, tool command with cwd, errors in danger color,
  final usage block), word-wrapped, scrollable, `esc` back. Missing
  transcript file (pruned, moved) renders a named
  "transcript unavailable at <path>" line — no crash, no fake data.
- **Task detail line** (existing TASKS section): gains a runs suffix
  when the card has runs: `2 runs · $0.42` (or `2 runs · cost
  partial`); `r` on the card jumps to the newest run's replay.
- All panes reuse kit Panel/Center + theme glyphs; new goldens.

## Part C — doctor

- `runs/store`: malformed `[[run]]` lines or unreadable transcript
  dirs warn by name (consistent with the tasks/sessions malformed-
  card rows); absent runs dir = OK (nothing recorded yet).

### Acceptance criteria

- Schema: run records round-trip through task TOML for both runtime
  kinds; unknown status values refuse with the value named (strict,
  like every other decode in the repo).
- Rollups: table tests over fixture record sets — costed + cost-less
  mixes produce the exact sums and the "partial" marker; `-1` tokens
  never leak into totals as zero (they are excluded and the row is
  marked).
- Profile: golden with mixed runs (ok/failed/timed_out, costed and
  not); totals line matches the table-test math.
- Replay: golden over a fixture transcript (tool command line, error
  line, usage footer); missing-transcript path renders the named
  refusal line (test).
- Task detail: runs suffix appears exactly when runs exist; `r`
  routing opens the newest run (test).
- Doctor: malformed run row warns naming the card + line; JSON
  output includes the row.
- `make verify` green.

## Deferred

- A `cli:<name>` model/cost schema *per CLI* (each adapter's terminal
  usage shape; claude's is parsed today, the rest land with waves 2–3
  of F-013) — its own spec when the conformance fixtures carry it.
- Cost per *model* breakdown and trend charts (data is already
  recorded; a rendering question).
- Transcripts in the reviewer surface alongside the diff (natural
  M9: run + diff + transcript in one review).
