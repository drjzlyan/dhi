# F-127 — Context and cost meter in the chat header

Status: done · gap G-B1

Each agent conversation (an episode) already records `LastContext` (the latest call's prompt size)
and the spend so far (`Turns`, tokens, `CostUSD`), but the chat never showed them. The user could
not tell a conversation was nearing its limit, or what it had cost, without opening a card.

## Decision
The limit a conversation is measured against depends on how its next turn is built. A kept CLI
session resumes at `LastContext`, so it is bounded by `memory.session_tokens`. A conversation
without a session is rebuilt each turn from the assembled prompt, bounded by `memory.prompt_tokens`
(the budget the runtime fits to). The meter shows the one that applies.

## Behaviour
- A DM header shows `ctx 18k/160k 11% · 12k in 3k out · $0.12` after the agent's name.
  Channels show nothing: several agents share them and no single episode speaks for the view.
- With a thread open the meter is that thread's episode; otherwise the DM's most recently
  updated episode.
- The ctx part turns warning from 80% of the limit and danger from 100%. Unknown context (0) drops
  the ctx part; a conversation with no turns shows nothing.
- Cost is omitted when zero (CLIs that report none). If the header has no room, the tokens drop
  first, then the whole meter; the agent name and presence are never truncated.
- Reads are cached for one second so redraws do not re-read episode files.

## Acceptance
- Text for resumed vs fresh limits, percent, warning and danger tiers
  (`TestMeterText`, `TestMeterTiers`).
- The DM header carries the meter, a thread uses its own episode, channels show none
  (`TestHeaderShowsMeter`).
- Narrow widths drop the tokens, then the meter, keeping the name (`TestMeterFitsWidth`).
