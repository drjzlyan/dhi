# F-142 — Autosave

Status: done · gap G-R1

Edits stayed in memory until `:w`. A crash or a forgotten save lost them.

## Decision
Autosave is a Settings choice, `editor.autosave`: `off` (default), `delay`, `focus`. It calls
the same `Editor.Save` as `:w`, so the outside-change check, format-on-save and the LSP didSave
(F-132) all still apply. It never overrides the changed-on-disk guard.

## Behaviour
- Settings row "autosave" cycles off, after delay, on focus change; it applies live (read through
  `Prefs` like F-116).
- `delay`: a modified buffer is saved once the editor has seen no key for 1 s
  (`autosaveDelay`), checked on a 500 ms tick. Typing in insert mode keeps postponing it.
- `focus`: the buffer is saved when a key moves focus off it: another tab or split pane, or back
  to the tree. Switching to another surface is not detected.
- If the file changed on disk, nothing is written. The buffer says "autosave stopped — changed on
  disk; :w! keeps yours, :e! loads it", and autosave stays off for that buffer until it is clean
  again (`:w!` or `:e!`). A write error stops it the same way.
- Buffers without a path and the commit-message buffer (F-121) are never autosaved.
- The clock is injectable (`editor.WithClock`, `Model.now`) so tests are deterministic.

## Acceptance
- Default is `off`; bad values are refused by name (`TestAutosaveSetting`).
- The row cycles (`TestAutosaveRowCycles`).
- `TestAutosaveOffNeverSaves`, `TestAutosaveDelayWaitsForQuiet`, `TestAutosaveRunsSaveHooks`.
- `TestAutosaveStopsWhenChangedOnDisk`: outside file untouched, message shown, stops, resumes
  after `:w!`.
- `TestAutosaveFocusSavesOnLeavingBuffer`, `TestAutosaveFocusSavesOnTabSwitch`.
- `TestAutosaveSkipsCommitMessage`, `TestAutosaveSkipsUnboundBuffer`.
