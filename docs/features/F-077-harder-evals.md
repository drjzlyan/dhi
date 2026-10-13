# F-077 — Harder evals that exercise fix-and-retry

Status: done in code (M30 P6) · live run pending (spends real tokens; run on request)

## Problem
Every eval fixture passed DHI's verify on the first round, so the
verify → fix → re-verify loop (F-067) was never measured, and results did
not record verify rounds at all. Only Go fixtures existed.

## Behaviour
- **Results** carry `verify_rounds` and the final `verify` verdict, read
  from the DHI arm's episode. The live log prints rounds next to the
  rounds the fixture is built to need, and the summary table has a
  `rounds` column.
- **Fixture keys:** `verify` (the member's verify command for the DHI arm,
  through F-076's workspace override), `max_iterations` (round cap) and
  `expect_verify_rounds` (reported, never scored).
- **New fixtures:**
  - `fix-retry-hidden` (Go): the prompt names one bug (refunds added in
    `ledger.Total`); DHI's verify also runs build-tagged report checks
    that catch an int32 overflow the prompt never mentions. A fix for the
    named symptom alone fails verification once (checked by hand: the
    sign-only fix passes `go test` and fails `go test -tags verify`), so
    the retry loop has to finish the job.
  - `multi-bug` (Go): three independent bugs (unicode truncation,
    rounding, overflow); the prompt names one symptom.
  - `py-fix-failing`: a uv project verified with unittest (F-076).
  - `ts-fix-failing`: TypeScript run by node's test runner with native
    type stripping, no dependencies (F-076).

## Acceptance
- [x] Fixtures load, stage and fail on their seeds.
- [x] The verify override reaches the member; the round cap reaches the
      tuning; the table reports rounds.
- [ ] Live: `DHI_EVAL=1 DHI_EVAL_TASK=fix-retry-hidden make eval` shows
      rounds ≥ 2 on the DHI arm (pending the user's go-ahead).
