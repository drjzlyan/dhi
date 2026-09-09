# F-018: Agents in Settings — full CRUD, live roster

Status: planned (M10 P1) · Milestone: M10 · Depends on: F-006 (settings),
M2 (org crew ops). Inspired by: the user goal — "a user can create their
own agents in settings".

## Summary

Agent management lives in the workspace ORG pane today, and — a real
gap this feature fixes — `runtime.Reload` has no callers: an agent
created mid-session is not mention-addressable until DHI restarts.
F-018 brings full agent CRUD into the Settings surface and wires
roster changes to the live runtime.

## Part A — Settings gains sections

The Settings surface keeps its config rows as the CONFIG section and
gains an AGENTS section via the same `[`/`]` switcher the workspace
uses:

- AGENTS rail: one row per rostered agent — `id · model · runtime ·
  N tools`, archived agents listed dim with an `archived` marker.
- Keys: `n` new, `e` edit selected, `a` archive (or restore, when on
  an archived row), `x` delete-with-confirm, `j/k` select.
- The create/edit form covers every manifest field: id, name, model,
  system prompt, runtime (toggle over the registered CLI names —
  strict enum), tools (comma-separated, validated against
  `manifest.BuiltinTools` + registered runtime names). Validation is
  the manifest package's own (strict, unknown tool named); errors
  render in the form, nothing half-writes.

## Part B — CRUD via the org crew seam (no new storage)

All writes go through the existing org crew ops so hand-edited
rosters and UI-made changes converge on identical on-disk state
(`.dhi/agents/*.toml` stays the single identity store):

- create → `org.CreateAgent` (duplicate id and archived-id refusals
  keep their messages); edit → `org.UpdateAgent`; archive/restore →
  `org.ArchiveAgent`/`RestoreAgent`; delete → NEW `org.DeleteAgent`
  (removes the manifest file; archive stays the soft path — the
  confirm modal says which one you asked for).

## Part C — live roster (the reload pump)

A single pump in cmd/dhi: subscribe to the workspace agent-tree
change signal, reload `org.LoadRoster`, and call `runtime.Reload`
(already atomic — entries rebuilt before the swap; failures refuse
with the manifest error named and keep the previous roster). This
makes ORG-pane and Settings changes — and hand edits — go live
without a restart, closing the dead-seam gap.

Degrades (F-011): no runtime wired → the AGENTS section still lists
roster state from disk with a visible "runtime unavailable — changes
apply on next launch" hint; create/edit still write (disk is the
truth), reload failure is a named flash, never silent.

## Acceptance criteria

- CRUD: create/edit/archive/restore/delete each round-trip through
  `.dhi/agents/` (strict manifest decode after every op); duplicate
  create refuses naming the id; delete confirm modal names archive as
  the soft alternative.
- Live: creating an agent then @-mentioning it in a bus message
  routes a turn WITHOUT restart (pump test with a stub CLI); a
  manifest that fails validation leaves the previous roster running
  and surfaces the error.
- Settings: section switching, form validation errors render,
  archived rows restore; goldens for the AGENTS section (populated +
  empty).
- `make verify` green.

## Deferred

- Per-agent env/secrets editing in Settings (declared env is a
  runtime-CLI concern, ADR-0012 — revisit with a secrets design).
- Drag-to-reorder roster display.
