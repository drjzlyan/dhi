# F-131 — Stop a looping or over-budget run while it streams

Status: done · gap G-C4

Budgets bound only between spawns (F-066): a CLI that retried one failing tool call, or ran past
`budgets.*`, kept going until its own limit. Nothing noticed a loop at all.

## Decision
Stop through the watchdog's own cancel (`stopSpawn`, as for stall and relaunch), name the reason on
the run record and to the human, and never retry (a retry repeats the loop or the spend). The
loop thresholds are fixed constants (`clirun.LoopRun`, 8 identical calls in a row; or 12 of the last
24 when those 24 are at most two distinct calls), not a new setting.

## Behaviour
- Adapters expose `StreamEvent.Call` (tool + canonical input) for claude, codex (commands) and
  opencode. Other adapters give no identity, so their calls are never counted; `Detail` is a
  truncated display line and is not used.
- Same call 8 times in a row, or 12 times within 24 calls that hold at most two distinct
  calls (A/B ping-pong; an edit-then-test loop runs on, `TestLoopGuardEditTestIsNotALoop`): the run ends with
  `Status=error`, `StopReason="loop"`, `Error="stopped by DHI: the same tool call N times ..."`.
- `StreamEvent.Spend` carries streamed usage: claude per-message tokens (deduped by message id),
  opencode per-step tokens and cost. The runtime adds it to the ledger tally and checks every
  `budgets.*` limit as it streams. At the cap: `StopReason="budget"`, the error names the limit
  and its setting.
- USD caps bind in flight only where the CLI streams cost (opencode). Claude reports cost only in
  its final line, so for it the token cap (`budgets.task_tokens`) is the in-flight guard and USD
  caps still bind between spawns.
- A stopped run is charged to the ledger with what was streamed (tokens in `TokensIn`).
- The human gets an alert (`I stopped: ...`), with the Settings pointer for a budget stop.
  `clirun.FailGuard` is not retryable.

## Acceptance
- `TestRepeatedToolCallStopsTheRunByName`, `TestDistinctToolInputsAreNotALoop`
- `TestInFlightSpendStopsAtTheCap`, `TestInFlightSpendBelowTheCapRunsOn`
- `TestToolCallNormalizesInput`, `TestLoopGuardConsecutive`, `TestLoopGuardWindow`,
  `TestLoopGuardStreakResetsAndIgnoresUnnamedCalls`, `TestGuardStopIsNotRetried`
- `TestClaudeToolUseCarriesFullInputIdentity`, `TestOpencodeStepFinishStreamsSpend`
