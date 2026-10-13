# F-070 — Settings proposals: agents ask, the human applies

Status: done (M29 P6)

## Problem
Agents hit settings that were in their way: a budget too tight for the
task, a scope that kept asking, too few verify rounds. They had no way to
say so except prose that got lost. The user asked for exactly this: an
agent changes a setting "after checking with the user".

## Behaviour
- **`settings_propose`** is a harness tool for every agent. It takes a
  key, a value, a layer (workspace by default, or user) and a required
  rationale.
  - The app checks the change against the whole layer stack
    (`settings.CheckChange`, wired through the `SettingCheck` seam
    because agentkit cannot import settings), so a proposal that would
    not load is refused at once.
  - A valid proposal is recorded pending in `.dhi/proposals/settings.json`.
    A newer proposal for the same key and layer replaces the old one.
  - The agent `@you`-mentions the human, which lands it in the Inbox.
- **Proposable keys.** `agents.*`, `budgets.*`, `scopes.*`, editor and
  terminal sizes, `worktrees.root` and `engine`. **Never**
  `security.sandbox`, `[keys]` or conventions.
- **Settings → PROPOSALS** lists settings proposals with the pending
  tools.
  - Each row shows `key old → new` and who proposed it.
  - `v` shows the reason and the layer.
  - `y` applies **exactly that one key** to the layer file
    (`settings.SetLayerKey`; every other key in the file is untouched,
    and defaults are never frozen into the layer), reloads the live
    config and records the approval.
  - `n` rejects. The record is kept either way.
  - Changes reach agents on their next spawn, because the runtime
    re-reads tuning every turn.

## Acceptance
- [x] `CheckChange` refuses unproposable keys, wrong types and values
      that fail validation. `SetLayerKey` writes one key and leaves the
      layer otherwise intact.
- [x] Store: a pending proposal for the same key replaces the old one;
      a bad layer refuses; a resolved proposal stays resolved.
- [x] Tool: records with the old value, @mentions the human, and refuses
      unproposable keys and a missing rationale without recording
      anything.
- [x] PROPOSALS: listed, viewed, applied (persisted and live) and
      rejected.
- [x] `make verify` and lint are green.
