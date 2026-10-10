# F-065 — Context hygiene, failure-aware retry, cache accounting, evals

Status: done (M29 P1)

## Problem
M29 aims for a harness that beats raw Claude Code and Codex on success,
cost per outcome and human interventions. Today the turn prompt works
against all three:

- **The wrong history.** `cliPrompt` reads `Bus.History(channel, 0)`,
  which is the channel's *top level*. A trigger inside a thread therefore
  sees unrelated channel chatter and never sees its own thread.
- **No memory of its own replies.** The agent's own earlier messages are
  skipped, so it cannot see what it already said or did. They were
  skipped because streamed progress lines are posted under the agent's
  name and would flood the prompt.
- **No task context.** The bound task card (status, comments, earlier
  run outcomes) never reaches the prompt.
- **Cache-hostile order.** Volatile memory and KB hits sit between stable
  blocks (standards, conventions, workflow), so every turn changes the
  system prefix and defeats the CLI's prompt cache.
- **Blind retries.** A retry resends the identical prompt after a fixed
  30s wait. An auth failure retries pointlessly. A rate limit retries too
  soon. An oversized prompt fails the same way again.
- **Cache use is invisible.** Cache-read tokens go unrecorded, so cost
  claims cannot be checked.
- **No measurement.** Nothing compares DHI against the raw CLI.

## Behaviour
- **Stream marking.** `bus.Message` gains `Kind` (`""` = a message,
  `"stream"` = a progress, command or error line relayed from a CLI run).
  The runtime marks the lines it relays. Prompt history drops stream lines
  and keeps the agent's real replies.
- **Thread-scoped history.**
  - A threaded trigger sees its thread: the root message plus the replies.
  - A top-level trigger sees the channel's top level, as before.
  - The agent's own replies appear as `you (earlier)`. The human stays
    `you (the human)`.
  - The window and per-message caps come from a compaction level (below).
- **Task card in the prompt.** When the trigger is bound to a card, a
  `Task card:` block lists:
  - the slug, title, status, priority, labels and workflow;
  - whether tests pass;
  - the last comments;
  - the last runs, with status and summary or error.
- **Stable → volatile order.** The system block runs: composed persona →
  grounding → standards → conventions → workflow → memory → KB. The
  Settings preview's "system, then standards, then conventions" order is
  unchanged.
- **Compaction levels.**
  - Level 0 is the normal prompt.
  - Level 1 is used after a context-overflow failure. It keeps fewer
    history messages with shorter caps, the last 4 journal entries and
    shorter notes, the single best KB hit, and fewer card comments and
    runs.
  - Each elided block says so in one named line. Nothing is dropped
    silently.
- **Failure classes.** `clirun.Classify(status, errText, events)` names:
  - `auth`: not logged in or a bad key;
  - `rate_limit`: 429, overloaded or a usage limit;
  - `overflow`: the prompt or context is too long, or the argv budget is
    exceeded;
  - `timeout`;
  - `no_final`;
  - `crash`: anything else.
- **Failure-aware retry.**
  - `auth`: no retry. A named notice says how to log in.
  - `rate_limit`: exponential backoff with jitter, starting at the base.
  - `overflow`: retry immediately at compaction level 1.
  - `timeout`: no retry (the turn's deadline is spent).
  - `crash` and `no_final`: retry after the base backoff. The retry prompt
    carries a failure brief: the class, the error and the last transcript
    lines, plus "continue; don't redo finished work".
  - The visible notice keeps its `retrying (attempt n/N)` prefix, which
    the reviewer relies on.
- **Cache accounting.** `clirun.Usage` and `tasks.Run` gain `CacheRead`
  and `CacheWrite` (0 means not reported). Claude parses
  `cache_read_input_tokens` and `cache_creation_input_tokens`; codex
  parses `cached_input_tokens`. Run replay and rollups show them.
- **Evals (P1b).**
  - `internal/evals` loads fixture tasks from `testdata/evals/<name>/`
    (`task.toml` plus a seed tree). Each task names the files and strings
    that must exist afterwards, and a token budget.
  - It runs each task in two arms: `raw`, the bare CLI headless in a temp
    copy; and `dhi`, through the runtime with the full harness.
  - It scores success, tokens, cache reads, cost, wall time and retries,
    and appends the results to `.dhi/evals/scorecard.jsonl`.
  - Offline mode uses fake-PATH stubs and runs in `make test`. Live mode
    runs only with `DHI_EVAL=1` (`make eval`).
- **Persisted rollups: dropped.** `tasks.RollupRuns` is pure arithmetic
  over runs the card already stores, so there is nothing to persist
  (YAGNI).

## Acceptance
- [x] A threaded trigger's prompt contains its thread (root and replies)
      and none of the channel's other top-level messages.
- [x] The agent's final replies appear as `you (earlier)`. Its stream
      lines never appear.
- [x] A trigger bound to a card carries `Task card:` with title, status,
      the last comment and the last run outcome.
- [x] The system block order is pinned by a test: standards < conventions
      < workflow < memory < KB.
- [x] `Classify` is table-tested over real-looking error text for every
      class.
- [x] Runtime tests on stubs cover four cases:
  - [x] auth: no retry, plus a named notice;
  - [x] overflow: the retry prompt is compacted;
  - [x] crash: the retry prompt carries the failure brief and transcript
        tail;
  - [x] rate_limit: the backoff grows.
- [x] Claude and codex Finalize report cache tokens from fixture lines.
      They are recorded on `tasks.Run` and shown in run replay.
- [x] (P1b) The offline eval suite runs in `make test`.
- [x] (P1b) Live: `make eval` (`DHI_EVAL=1`) writes a scorecard row per
      task and arm against a real CLI. See the baseline below.
- [x] `make verify` is green.

## Notes
- **One token invariant.** `TokensIn` is fresh input only, so `TokensIn
  + CacheRead + CacheWrite` is the whole prompt for every adapter. Codex's
  `input_tokens` includes cached input (codex-rs `TokenUsage.
  non_cached_input`), so its adapter subtracts it. Cache writes are
  counted in every eval figure because they are billed.
- **Classification input.** `Classify` reads only the run error (the
  Finalize reason or the stderr tail). Stream error lines are mostly the
  agent's own failed tool commands. A `git push` that printed
  "Authentication failed" must not report the CLI as logged out.
- **Reply marking.** Own messages replay only when they carry
  `Kind: "reply"`. Positive marking keeps legacy unmarked progress lines
  out of the replay.
- **Eval order.** Arm order alternates per run, and `DHI_EVAL_REPEAT`
  sets the number of runs. A single run is an anecdote, not evidence.
- **Claude error reasons.** Claude's error result line carries the reason
  in `result`, but `Finalize` used to keep only the subtype, so every
  failure looked like a generic crash. It now keeps the reason. The
  runtime also records the last line of stderr when a CLI dies without a
  final line.
- **Fair comparison.** Both eval arms run without the OS sandbox, with the
  same environment (PATH plus the CLI's declared pass-through) and the
  same model. The DHI arm auto-approves asks and counts them. That is the
  baseline for P3's autonomy envelope.
- **Deferred to P3.** An in-IDE scorecard view is left for the Agent HQ
  work. Until then the scorecard is the JSONL file plus the `make eval`
  table.

## Live baseline (2026-10-09)
Run: claude 2.1.x with its default model, `DHI_EVAL_REPEAT=3`, two
fixtures, 12 runs in all.

| arm | success | tokens/ok | cached | cost (6 runs) | mean wall |
|---|---|---|---|---|---|
| raw | 6/6 | 94.7k | 72% | $1.41 | 19.9s |
| dhi | 6/6 | 156.9k | 86% | $1.35 | 37.2s |

What it means:
- **Ceiling.** Both fixtures are too easy to separate the arms on success.
  P2 must add harder fixtures before any "better" claim: multi-step work,
  repo conventions, red tests to fix, and recovery from a mid-run failure.
- **Cost.** DHI is about 4% cheaper per run despite a ~65% larger prompt.
  The stable-first order puts 86% of it in the cache, and it writes less
  cache than raw (about 20.5k against about 25.7k).
- **Speed and output.** DHI is about 1.9× slower and writes about 2× the
  output tokens (1.6–1.8k against 0.7–0.9k). The likely causes are the
  workflow block and the served-tool round trips. That is the first
  efficiency target for P2/P3.
- **First live run.** It failed both arms with "Not logged in". The eval
  had stripped the environment, and on macOS claude needs `USER` to find
  its Keychain login. The app passes the host environment, so it is not
  affected, and the eval now does the same. `Classify` handled it
  correctly: auth was not retried and a notice was posted.
