# ADR-0031: Episodes, CLI session resume, and spend budgets

Date: 2026-10-13 · Status: accepted · Companion to: ADR-0019 (DHI owns
the loop, the CLI is the engine), ADR-0011 (no silent fallbacks),
ADR-0024 (CLI engines only). Serves: F-066 (M29 P2).

## Context

Every agent turn was a cold spawn. The prompt re-sent the conversation,
and the CLI rebuilt its whole context from nothing on each turn of the
same task. Host CLIs keep their own sessions: `claude -p --resume <id>`
and `codex exec resume <id>` continue a conversation with its context
cached. DHI never used them. Spend was recorded per run but never
capped.

## Decision

1. **Episodes are DHI-owned working state.** An episode is keyed by the
   bound task card, or by the conversation thread when there is no card.
   It lives at `.dhi/episodes/<agent>/<key>.json` and records:
   - the CLI session id, the CLI and the cwd it ran in;
   - the newest bus message the session has seen;
   - turns, cumulative tokens and cost;
   - the last outcome.

   It is engine-neutral. A CLI without verified resume gets the
   episode's compact summary instead of a session.
2. **Resume only where it is verified.** An adapter may set `ResumeOK`
   only after a live verification: recall across spawns, plus cache
   reads on the resumed turn. Claude (2.1.285) and codex (0.147.0) are
   verified. The rest start fresh, and doctor says so per CLI
   (`runtime/<cli>/resume`).
   - A session resumes only on the same CLI in the same workdir.
   - A resumed turn replays only the messages the session has not seen.
3. **A dead session is never silent.** When a resumed spawn fails with a
   retryable class, DHI posts a named notice and makes one fresh spawn
   with the full context. That spawn is outside the retry budget, because
   a pruned session is DHI's problem, not the task's.
   - A failed attempt that reported a session (e.g. it ran out of turns)
     resumes that session on retry, continuing the work rather than
     restarting it.
   - A context overflow starts fresh and compacted.
4. **Budgets bind between spawns.**
   - Every run appends to `.dhi/spend.jsonl` (agent, task, tokens, USD),
     card or not.
   - Before each spawn, the ledger is tallied against `[budgets]`:
     `task_usd`, `task_tokens`, `agent_day_usd` and `workspace_day_usd`.
     Zero means no limit.
   - On a breach the spawn refuses with a notice that names the limit and
     the setting that raises it.
   - A CLI is never cut off mid-run.
   - An unreadable ledger refuses by name rather than skipping the check.
5. **`agents.max_turns` caps one spawn's inner loop.** It defaults to 0,
   which means the adapter's 50, so DHI can take control back more often
   (P3's goal loop lowers it). `agents.resume` defaults on.
6. **The feature workflow governs task work only.** A chat turn with no
   bound card carries no worktree/test/commit/PR steps. The live eval
   showed agents searching for a branch to push while answering plain
   requests.

## Consequences

- Settings live: the runtime re-reads `[agents]` and `[budgets]` per
  turn, so a CONFIG edit applies from the next spawn.
- `tasks.Run.Session` records the session a run reported, and
  `clirun.Usage.SessionID` carries it out of `Finalize`.
- Episodes and the ledger are per-user runtime state (gitignored).
- The adapter docs carry the verification date and version.
  Re-verification is the gate for flipping `ResumeOK` on a new adapter.
