# F-084 — Settings parity: no config files to edit

Status: done (M31 P6)

## Problem
Several settings could only be changed by editing `config.toml` or
`conventions.toml`: the default engine, the sandbox, the reflection model,
key remaps, editor languages, the commit co-author, the copyright holder and
license, and the subject limit. Every preset row refused other values, and
the UI told people to "hand-edit config.toml".

## Behaviour
- **New CONFIG rows:** `editor.languages` (a switch per built-in language),
  `security.sandbox` (auto/off, warned), `conventions.copyright.license`,
  `conventions.commit.max_subject`, `engine` (cycles the registered coding
  CLIs), `agents.reflect_model`, and `keys` (now editable). Rows are listed
  in display order, so j/k walks them top to bottom.
- **Typed values.** `e` on any row with a value opens a one-field editor
  with a hint on what it takes (numbers range-checked, dollars accept `$`).
  The whole config is checked (`settings.Check`) before saving; a refused
  value stays in the editor with the reason. Co-author and copyright open the
  editor themselves when there is nothing to switch on yet; empty turns them
  off.
- **Keys.** `pressed=meant, …` in one field; ctrl+c and other invalid remaps
  are refused by name.
- **Labels.** Rows read in plain words ("budget per task", "may run
  commands", "writing tone"), not storage keys (`settingLabels`).
- **Copy.** No UI text names a config file any more: conventions say
  "shared with your team" or "just for you".
- **Guard.** `TestEverySettingHasAPlaceInSettings` walks `settings.Known()`
  against the keys CONFIG rendered (`cfgKeys`) and fails for any key with no
  row and no named owner (template keys
  belong to CONVENTIONS).

## Not done
- Workflows, packs and workspace dependencies still point at their files;
  they are not part of `config.toml`.

## Acceptance
- [x] Typed values: in range saved, out of range refused, dollars, sandbox, reflect model persisted
- [x] Keys and languages editable and persisted; ctrl+c remap refused
- [x] Co-author / copyright ask for the value instead of naming a file
- [x] Guard test fails for a key with no place (checked by adding one)
