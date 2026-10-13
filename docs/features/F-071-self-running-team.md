# F-071 — A self-running team

Status: done (M29 P7)

## Problem
Teams existed (ADR-0025: hop-budgeted delegation, leads), but nobody
split work. Delegation was prose. Nothing sequenced dependent work.
Nobody reviewed an agent's PR before the human did. There was no view of
how each "employee" was doing.

## Behaviour
- **Decomposition.**
  - Cards gain a `parent` and `depends_on`, and a `held` flag for a brief
    not yet handed off. Cycles and unknown cards are refused.
  - `task_create` takes `parent` and `depends_on`. With open blockers the
    brief is held. Otherwise it is handed off (team channel, else DM) and
    the card is bound to it.
  - DHI releases each held card the moment its blockers are done. This
    happens after every turn and on any card change (`WatchTasks`), so a
    board move by the human counts too. The release posts as `dhi`.
- **Lead prompt.** The lead of a card's team gets the roster with roles
  and an invitation to split the card into sub-tasks. A parent card
  shows `sub-tasks n/m done`, and its PR is refused until all of them
  are done (a gate at `pr_open`). A sub-card shows its parent and its
  open blockers.
- **Structured hand-off.** An agent woken by another agent's message
  gets the hander's working state from its episode: goal, plan steps,
  DHI verify verdict and last outcome. It no longer reconstructs that
  from prose.
- **Reviewer loop.** When `pr_open` succeeds, a rostered agent with the
  `reviewer` role (preferring the card's team, never the author) is asked
  in the card's thread to review before the human does. Its findings land
  where the author's next turn reads them.
- **Agent HQ.** The agent profile gains a PERFORMANCE record:
  - tasks shipped, run success rate, DHI-verified episodes;
  - a 7-day spend sparkline with cost per task;
  - lessons held (pinned) and tools forged.
- **Rituals.** Settings → AUTOPILOTS `t` adds a daily standup per team
  led by an agent and a weekly self-review per agent. They are added
  paused, so the human arms each one.

## Acceptance
- [x] Plan model: parent, dependencies, cycle refusal, ready and release,
      persistence.
- [x] `task_create`: held behind open dependencies, otherwise handed off
      and bound; self-dependency refused.
- [x] A released card is handed off by `dhi`, bound, and its assignee
      dispatched.
- [x] Lead roster and split invite; parent progress; parent PR gate.
- [x] Hand-off brief carries goal, plan, verify and outcome.
- [x] The reviewer is chosen by role and team and never self-reviews;
      the review request is posted in the card thread.
- [x] Agent HQ record: only this agent, only this week; golden.
- [x] Rituals added paused, once.
- [x] `make verify` and lint are green.
