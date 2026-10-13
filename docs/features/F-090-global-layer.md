# F-090 — One global layer: set it once, every workspace has it

Status: done (M32 P5) · ADR-0039

## Why
Every workspace had to be configured again. Worse, Settings saved the
whole merged config into `.dhi/config.toml`. Editing one row copied all of
your own values into the workspace, where they froze: a later change to
your config never reached it.

## Behaviour
- **One key at a time** (`settings.Snapshot` / `SaveChanged`). `scopes`
  merges as a whole table, so a change to one scope writes the whole
  effective table (otherwise yours would be dropped), and the repair
  treats it as one unit. A save
  writes only the leaves that differ from the last saved snapshot,
  removed map entries included, to one layer file. Everything else in
  that file stays as it was.
- **Where CONFIG edits go.** Profile rows (you) always save to your
  config. Other rows save to this workspace by default; `s` switches to
  "every workspace", and the hint bar says which is active.
  - Saving for every workspace also drops this workspace's own value for
    the same key, so yours governs here too.
  - A row whose value comes from this workspace is marked
    `· this workspace`.
- **Default engine.** The wizard's `SetEngine` saves it to your config
  (it is your CLI) and removes the workspace copy. Settings can still pin
  a workspace to its own engine.
- **Repair `config-copies`** (safe, at launch). It finds workspace values
  equal to yours (`settings.DuplicateKeys`), the leftovers of the old
  save, and removes them by name. Doctor lists it under `repairs/`.
- **Your standing instructions** (`standards.UserPath` =
  `<config>/dhi/standards.toml`, `everyone = [...]`). The order is
  built-ins, then yours, then workspace, team and agent; a `replace` agent
  layer still discards everything upstream except built-ins.
  - `Check` validates the file.
  - Settings → STANDARDS has a `you · every workspace` row, edited with
    `u`.
  - The path is set by the app at startup; empty in tests, so they stay
    hermetic.
- **Wizard step `instructions`.** It asks "What should every employee
  always do?" (rules separated by `;`), saved for every workspace by
  default or this workspace. A finished setup is offered it once
  (catch-up).
- **Already global before M32:** profile, git identity, credentials, setup
  state. A secret can now be saved for every workspace too (F-089), and so
  can a grant (F-087).

## Not done
- Integration cards (`.dhi/mcp/*.toml`) stay per workspace. Their
  credentials are already global.
- The wizard's conventions step still saves to the workspace when one
  exists. Conventions are tracked team files, shared with collaborators,
  so a per-person default would surprise them.

## Acceptance
- [x] SaveChanged writes only the edited key; user values stay out of the workspace file
- [x] CONFIG: default this workspace; s → every workspace removes the workspace override; profile → you
- [x] config-copies repair removes only values equal to yours
- [x] user standards apply before the workspace's, in any workspace; malformed fails Check
- [x] wizard instructions: saves for every workspace by default; empty saves nothing; offered in catch-up
- [ ] live: set engine + an instruction for every workspace; a fresh second workspace has both without asking (M32 P7)
