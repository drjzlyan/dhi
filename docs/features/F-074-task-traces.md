# F-074 — Task traces for debugging

Status: done (M30 P3)

## Problem
Chat now shows only conversation (F-072), but debugging an agent needs the
rest: which runs and retries happened, what each CLI ran, what DHI's
verify loop said, and what the agent was sent. The data existed in
pieces: run transcripts with no timestamps, episodes, and bus threads. The
UI replayed only the newest run.

## Behaviour
- **Richer transcripts.** Every run attempt's JSONL starts with a
  `prompt` record (prompt and system sizes, resumed session, model). Each
  event carries its arrival time and attempt. A failed run ends with its
  stderr tail. With `agents.trace_prompts` on (Settings → CONFIG, default
  off), the full system and prompt text are saved too. Older transcripts
  still load.
- **One timeline.** `internal/agentkit/trace` merges, in time order: the
  card's runs (start, transcript events, outcome with duration, tokens,
  cache and cost), the bound thread with every kind of message (relayed
  copies of transcript lines are dropped; notices like retries and verify
  results stay), comments and activity.
- **Trace view.** `r` on a card (board or card page) and the inbox's
  failed-run jump open it.
  - A strip of run chips: all runs, then each run with agent, time and
    outcome. `n`/`p` select.
  - `f` cycles filters: everything, conversation, tools, problems,
    prompts. `/` searches text and actor.
  - `x` exports the visible events to `.dhi/traces/<slug>-<time>.md` and
    `.jsonl` (gitignored) to share or attach to a bug.
  - A transcript that was pruned is named, never shown as empty.

## Acceptance
- [x] Transcript records carry time, attempt, prompt sizes, stderr tail;
      legacy records parse.
- [x] Trace merge order, relay de-duplication, filters, Markdown/JSONL
      export are unit-tested.
- [x] View goldens, filter/search/export flow, background sweep.
