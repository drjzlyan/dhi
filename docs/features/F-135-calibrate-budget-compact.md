# F-135 — Calibrated prompt budget, model summary, /compact

Status: done · gap G-B2

`memory.prompt_tokens` is a token budget but prompts are trimmed in runes at a fixed four per token.
Code, CJK and dense text run well under four, so a "24k token" prompt could really be larger. There was
also no way to compact a long conversation on demand, and the episode kept only the last reply's first
400 characters as its summary.

## Decision
- Calibrate from what the CLI reports, per CLI and model. After a fresh (non-resumed), successful spawn
  with a reported per-call context (`Run.Context`), DHI pairs the real input tokens with the runes it sent.
  The CLI adds fixed overhead of its own (system prompt, tool definitions), so a plain tokens/runes quotient
  would blame it on the text. The fit is a decayed least-squares line (tokens = runes/ratio + overhead): the
  slope is the ratio, the intercept the overhead. A fit needs 3+ recent samples of differing prompt sizes;
  otherwise the ratio is 4. The ratio is clamped to 2..6. State lives in `.dhi/calibration.json`
  (DHI's own folder, not a config file); an unreadable file keeps 4 and the failure is named in the thread.
- The compaction summary runs on `agents.reflect_model` (already declared, shown in Settings and read by
  reflection); empty = the conversation's model, else the agent's. No new setting.
- `/compact` writes a model summary into the episode and drops the kept CLI session. The next turn starts
  fresh from the summary and replays only messages after it (`Episode.SummaryUpTo`). It is not a turn:
  turn count and spend of the episode are untouched (the spawn's spend is in the ledger).

## Behaviour
- Budget = `memory.prompt_tokens` x calibrated ratio x the compaction-level factor. No data = as before.
- `/compact` in a focused DM composer with the thread open: runs in the background; the runtime posts
  "compacted: N messages summarised…" or "could not compact: <reason>" (no conversation, budget reached,
  CLI error, empty summary). It waits for a turn in flight. Refuses by name in a channel, with no thread
  open, or without an episode.
- The summary is rendered in a fresh turn inside an `<untrusted_data>` fence (it is model-written from
  conversation text). `/fork` carries the summary; the cut-off is not carried (its thread is new).
- Prompts of episodes without a summary are byte-identical (`TestPromptGoldens` unchanged).

## Acceptance
- Slope fit separates CLI overhead; defaults without data, few samples or no spread; clamps; persists;
  corrupt file named (`TestCalibrationFitsSlopeNotOverhead`, `TestCalibrationNeedsDataAndSpread`,
  `TestCalibrationClampsToSaneBounds`, `TestCalibrationPersistsInDHIFolder`,
  `TestCalibrationCorruptFileKeepsDefaultAndNamesError`).
- Only fresh successful runs with a reported context are sampled, keyed by the conversation's model
  (`TestCalibrateSamplesOnlyFreshReportedRuns`); the budget follows the ratio
  (`TestPromptBudgetUsesCalibratedRatio`).
- Compact runs on `reflect_model`, stores the summary, drops the session; the next turn is fresh, carries
  the summary and skips covered messages; refusals are named (`TestCompactSummarisesAndNextTurnStartsFromSummary`,
  `TestCompactRefusesByName`, `TestCompactedDropsSessionAndRendersSummary`).
- Composer: `TestParseCompactCmd`, `TestCompactCommandAsksTheRuntime`, `TestCompactCommandRefusals`.
