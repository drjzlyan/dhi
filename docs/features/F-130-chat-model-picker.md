# F-130 — Pick the model for one conversation

Status: done · gap G-B8

An agent runs on its manifest model everywhere. A user who wants a stronger (or cheaper) model for one
DM conversation had to edit the agent in Settings, changing every conversation.

## Decision
The override is a field on the conversation's episode (`episode.Episode.Model`, the same
`.dhi/episodes/<agent>/<key>.json` the meter reads), not a new file. The runtime reads it at each
turn; the chat owns it, so a turn's episode save keeps whatever is stored. The model name is free text
(the CLI validates it); DHI keeps no model catalogue.

## Behaviour
- In a DM composer, `/model` shows the current model, `/model <name>` overrides it for this
  conversation, `/model clear` returns to the agent's configured model.
- The target is the open thread's episode, else the DM's newest one (same rule as the F-127 meter).
  With no conversation yet it refuses by name; in a channel it refuses by name. A name starting with `-` is refused.
- The next turn's CLI argv carries the model (`--model` etc.); the run record and trace show the model used.
- The DM header shows `model <name>` beside the context meter while an override is set.

## Acceptance
- Override reaches the CLI, survives the turn save, and clearing removes it
  (`TestEpisodeModelOverrideReachesCLI`).
- `/model` parsing, set/show/clear, header chip beside the meter, channel and no-conversation refusals
  (`TestParseModelCmd`, `TestModelCommandSetsShowsClears`, `TestModelCommandNeedsConversation`).
