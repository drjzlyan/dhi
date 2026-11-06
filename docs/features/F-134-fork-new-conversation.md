# F-134 — Fork a conversation, start a new one

Status: done · gap G-B4

A DM conversation is a thread; its episode (`.dhi/episodes/<agent>/thread:<ch>:<root>.json`) holds the
carried state. There was no way to branch a conversation to try another direction, nor an explicit
"new conversation" in a DM.

## Decision
- A fork is a new thread: DHI posts a root message (author `dhi`) and copies the source episode under the
  new thread's key (`episode.Store.Fork`). Carried: goal, plan, model override (F-130), turn count and the
  last status/summary that `Episode.Render` gives a fresh turn. `ForkedFrom` records the source key.
- The kept CLI session id is dropped (CLI, Session, Workdir, SeenID cleared), so the fork's first turn
  spawns a fresh CLI session and rebuilds context from the summary. A CLI session cannot be safely shared:
  two threads resuming one session would interleave into a single transcript and each would see the
  other's turns. Spend and context size also restart at zero (they describe the source's session).
- The source thread and episode are never written.

## Behaviour
- In a focused DM composer: `/fork` forks the open thread and opens the fork; `/new` leaves the open
  thread so the next message roots a new conversation; `/new <text>` posts `<text>` that way.
- `/fork` needs an open thread whose episode exists (the agent has replied); otherwise it refuses by name.
  Both commands refuse by name in a channel.
- The focused-DM composer hints list `/fork` and `/new` beside `/model`.

## Acceptance
- Fork copies state, drops the session, leaves the source alone, refuses an existing destination or
  missing source (`TestForkCarriesStateAndDropsSession`).
- Command parsing, fork opens a new thread with a copied episode, refusals, `/new` behaviour
  (`TestParseConvCmd`, `TestForkCommandStartsThreadWithCopiedEpisode`, `TestForkCommandRefusals`,
  `TestNewCommandLeavesThreadOrPosts`).
