# ADR-0025: Agent-authored messages dispatch, under a hop budget

Date: 2026-10-08 · Status: accepted · Companion to: ADR-0011 (no silent
fallbacks), ADR-0019 (DHI owns the loop). Serves: F-036.

## Context

`Runtime.Handle` ran only for human-originated posts. An agent reply, or
a `channel_post` tool call, went straight to the bus, so an `@other-agent`
inside it woke nobody, and `org.Team.Lead` was stored but never read. A
team could not delegate: "create a team and ask it to do a task" worked
only if the human @mentioned every member.

## Decision

1. Agent replies and `channel_post` re-enter `Handle`. An @mention of a
   rostered agent (never the author) wakes it. Routing is by explicit
   mention only; an agent's bare message wakes nobody.
2. A **hop budget** (`maxAgentHops = 8`) bounds agent-authored dispatches
   per channel since the last human message. A human post resets it. At
   the limit the runtime posts a visible notice and dispatches nothing.
   The budget is per channel, not per thread, because a top-level reply
   and its trigger have different thread roots and a per-thread key
   would reset the chain.
3. A bare **human** post in a team channel `#<team>` goes to the team's
   lead when the lead is a rostered agent. A team with no lead gets a
   named notice (ADR-0011) rather than silence; a human lead stays quiet.
4. Setting an assignee/team on a card (board form, assign) posts the
   task brief to the bound thread, else the team channel, else the
   assignee's DM, and dispatches it. `task_create` accepts
   assignee/team/labels/priority and hands the brief to the assignee via
   the team channel.

5. **Ideation session channels are excluded** (`#ideation-<id>`,
   `ideation.IsSessionChannel`). The round-table's floor protocol already
   hands the floor between agents (moderator, ordered turns, a 24-turn cap)
   through `mirrorBus → advanceFloor → giveFloor`. Dispatching an agent's
   @mention in the runtime as well woke the addressee twice and bypassed
   that accounting (found in review; reproduced as 2 CLI spawns instead of
   1). Human posts in a session still route normally.

## Consequences

- A passing "@bob" in an agent's prose wakes bob. Agents are told to
  mention only to hand work off; the hop budget bounds the cost.
- No new persisted state: hops are in-memory and reset on restart.
- Chains are auditable: every hop is a bus message in the channel.
