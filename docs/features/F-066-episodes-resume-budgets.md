# F-066 — Episodes, CLI session resume, budgets

Status: in progress (M29 P2)

## Problem
Each turn of the same task re-sent the conversation to a cold CLI and
rebuilt the context from nothing. Spend had no caps. The feature
workflow (worktree → test → commit → PR) rode every turn, so chat
answers wandered into git chores; the P1 live eval showed this. The
eval also had no multi-turn tasks and no "tests must pass" checks.

## Behaviour
- **Episodes** (`internal/agentkit/episode`).
  - Each episode is keyed by task card, or by thread when there is none.
  - It holds the CLI session id, the CLI and workdir, the newest message
    seen, turns, tokens, cost and the last outcome.
  - A fresh spawn of an ongoing episode carries "This work so far: N
    earlier turns; the last one ended …".
- **Resume** (ADR-0031).
  - Adapters with `ResumeOK` (claude `--resume`, codex `exec resume`,
    both live-verified) continue the session.
  - A resumed turn replays only unseen messages.
  - A dead session gets one fresh spawn with a named notice.
  - A failed attempt that reported a session resumes it on retry.
  - Doctor shows `runtime/<cli>/resume` per CLI.
- **Budgets** (`internal/agentkit/budget`).
  - Every run appends to `.dhi/spend.jsonl`.
  - `[budgets] task_usd / task_tokens / agent_day_usd /
    workspace_day_usd` are checked before each spawn.
  - A breach refuses with a notice naming the setting.
- **Settings.** `[agents] max_turns` and `resume`, plus the four budget
  keys, are strict-validated. Settings → CONFIG has an "agents & budgets"
  group with preset cycles that saves live (re-read each turn).
- **Workflow scope.** The feature workflow is injected only on task-bound
  turns.
- **Bound-task lookup.** One `boundTask` helper matches on the thread
  root everywhere. Before, workdir and workflow used `trigger.Thread`, so
  a top-level trigger bound to a card got the workspace root.
- **Evals.**
  - Seeds are git repos, initialized with go-git.
  - `followups` adds multi-turn tasks: both arms continue their session,
    raw by resuming its own id.
  - `run` expectations (e.g. `go test ./...`) must pass.
  - New fixtures are `kv-followup` and `fix-failing`.
  - `DHI_EVAL_KEEP=1` keeps run dirs.

## Acceptance
- [x] The second turn of a thread spawns with `--resume S1` and does not
      replay what the session saw (runtime test on stubs).
- [x] A dead session falls back fresh with full history and a named
      notice, without spending retries.
- [x] With `agents.resume = false`, the turn is fresh and carries the
      episode summary. `agents.max_turns` reaches the argv.
- [x] A spent budget refuses before spawning, named in the error and the
      thread. Runs land in the ledger.
- [x] Chat turns carry no feature workflow; task turns do.
- [x] Claude and codex argv and session-id parsing are table-tested;
      only verified adapters claim `ResumeOK`.
- [x] Settings layer and validate the new keys; the CONFIG rows cycle,
      persist and say "next spawn".
- [x] Doctor has the resume row.
- [x] Offline eval: follow-ups resume in both arms, seeds are repos, run
      expectations are checked.
- [ ] Live eval across the four fixtures, recorded below.
- [x] `make verify` and lint are green.
