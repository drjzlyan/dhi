# ADR-0033: Agents learn lessons, automatically and reviewably

Date: 2026-10-15 · Status: accepted · Companion to: ADR-0007 (file-based
agent memory + shared KB), ADR-0031 (budgets), ADR-0032 (goal loop).
Serves: F-068 (M29 P4).

## Context

ADR-0007 gave each agent a journal and a notes file. Both change only
when the agent decides to write them, and the prompt replayed the last
few journal entries regardless of relevance. Nothing turned a failed
check, a retry or a human correction into a rule the agent applies next
time. The same mistake cost the same tokens every week.

## Decision

1. **Lessons are a separate, structured store.** A lesson is one short
   imperative rule with:
   - provenance (run ids), a confidence (0–1), a source (reflection,
     correction, agent or human), and uses;
   - a status: active, pinned or retired.

   Private lessons live at `.dhi/memory/agents/<id>/lessons.json`. User
   lessons (the human's preferences) live at
   `.dhi/memory/user-lessons.json` and apply to every agent. Near-
   duplicates merge rather than accumulate.
2. **Reflection is paid for only by a signal.** It runs after a turn with
   a retried attempt, a failed DHI verify, a stuck or capped loop, or a
   human correction (a reply such as "no…", "don't…", "…instead" to the
   agent's earlier reply).
   - It is one tool-less spawn of the agent's own CLI, capped at one turn.
   - It is budget-checked and ledgered like any run.
   - It may use a cheaper `agents.reflect_model`.
   - A clean turn never reflects.
3. **Lessons apply immediately and stay reviewable** (the user's choice).
   Each one is named in the thread ("learned: …"). Settings → BRAIN can
   pin it, retire it or share it to the KB. Sharing goes through the
   existing review policy (ADR-0007), so nothing reaches the team
   unreviewed.
4. **Retrieval, not recency.** A turn carries every pinned lesson plus
   the top few live ones by TF-IDF similarity to the trigger and task,
   weighted with confidence and recency (`knowledge.Similarity`, so there
   is one retrieval scheme).
5. **Consolidation is local.** Dedupe and decay run at most once a day
   per agent, at turn start, with no model call. Lessons unused for 30
   days with confidence under 0.5 retire. Pinned lessons never decay.
   Relearning a retired lesson revives it.

## Consequences

- Reflection adds at most one small spawn to a turn that already went
  wrong. That cost is visible in the ledger and capped by budgets.
- A wrong lesson is one keypress from retirement, and every lesson shows
  where it came from.
- The journal and notes stay as they were (agent-written). The prompt now
  carries only a short journal tail behind the lessons.
- Lessons used often with high confidence are flagged "standard?" in
  BRAIN, as candidates for coding standards (promotion stays manual).
