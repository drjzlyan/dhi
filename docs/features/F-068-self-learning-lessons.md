# F-068 — Self-learning: lessons, reflection on signal, consolidation, Brain

Status: done (M29 P4) · ADR-0033 — live eval of learning not yet run

## Problem
An agent's memory changed only when the agent chose to call
`memory_append`. The prompt then showed the last 8 journal entries,
whatever they were. Nothing turned a failure, a failed check or a human
correction into a rule the agent applies next time. Nothing deduped,
decayed or let the human curate what an agent "knows". The same mistake
cost the same tokens every week.

## Behaviour
- **Lessons** (`internal/agentkit/lessons`). A lesson is a short imperative
  rule. It records its provenance (run ids), confidence (0–1), source
  (reflection, correction, agent or human), uses and last use, and a
  status (active, pinned or retired).
  - Private lessons live in `.dhi/memory/agents/<id>/lessons.json`.
  - User preferences live in `user-lessons.json` and apply to every agent.
  - Adding a near-duplicate merges into the existing lesson (confidence
    rises, provenance accumulates). Relearning a retired lesson
    reactivates it.
- **Reflection only on a signal.** Reflection runs after a turn that had a
  retried attempt, a failed DHI verify, a stuck or capped loop, or a
  human correction.
  - A correction is a human reply such as "no…", "don't…" or "…instead",
    following the agent's earlier reply in the same thread.
  - The reflection is one short spawn of the agent's CLI: no tools, max 1
    turn, `agents.reflect_model` (default: the agent's model), and
    budget-checked and ledgered like any run.
  - It returns at most 3 lessons as strict JSON. They apply immediately
    (auto, reviewable) and are named in the thread: "learned: …". A
    malformed reply is named, never silent.
  - `agents.reflect = false` turns it off.
- **`lesson_record`.** A harness tool lets the agent record a lesson it is
  sure of while working: private, or user for the human's stated
  preferences.
- **Retrieval.** The memory block gains "Lessons you have learned (apply
  them)": every pinned lesson plus the top 5 live ones by relevance to
  the trigger and task (TF-IDF similarity, confidence and recency, via
  `knowledge.Similarity`). Retrieved lessons are counted as used. The
  journal tail shrinks to 3 entries.
- **Consolidation.** Local and token-free, at most once a day per agent,
  at turn start. It merges near-duplicates and retires lessons unused for
  30 days with confidence under 0.5. Pinned lessons never decay.
- **Brain** (Settings → BRAIN). Lessons are grouped by agent with the user
  preferences first. Each row shows a confidence bar, uses, source and
  age.
  - `p` pins or unpins, `x` retires or restores, `/` filters.
  - `s` shares a private lesson to the workspace KB, through the existing
    review policy.
  - Lessons used 5 or more times with confidence ≥ 0.8 are badged
    "standard?" as candidates for the coding standards.

## Acceptance
- [x] Store: add, dedupe-merge, retrieve rank with pins, user scope for
      all agents, retired never injected, relearn reactivates, decay.
- [x] The reflection prompt carries the evidence. The parser tolerates
      fences and prose, refuses missing JSON, clamps confidence and caps
      at 3. The correction detector is table-tested.
- [x] Runtime: a verify-fail turn reflects once and stores the lessons
      with run provenance; a clean turn does not reflect; a correction
      triggers it; the budget refuses it; `reflect = false` skips it.
- [x] The prompt carries retrieved lessons ahead of the journal; pinned
      lessons always appear.
- [x] `lesson_record` is served without an allowlist entry and stores a
      private or user lesson.
- [x] The Brain section lists, filters, pins, retires and shares lessons
      (golden + bg sweep).
- [x] `make verify` and lint are green.
