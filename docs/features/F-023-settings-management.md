# F-023: Settings owns management — TEAMS · PACKS · STANDARDS · AUTOPILOTS

Status: implemented (M11 P2, 2026-09-10)
(AGENTS section), F-008 (packs), M4-P2 (standards), F-015
(autopilots). Companion to ADR-0014.

## Summary

Agent/team management leaves the workspace. Settings grows from two
sections to six — CONFIG · AGENTS · TEAMS · PACKS · STANDARDS ·
AUTOPILOTS — becoming the single place where the crew and its rules
are created, installed, and edited. The workspace ORG/PACKS/STANDARDS/
AUTOPILOTS sections are removed (ADR-0014); INSPECT's profile view
moves to the Slack floor (F-022).

## Sections

### TEAMS (new — the only team UI)

- Rows: one per org team: `name · lead · N members`.
- `n` new team, `e`/`enter` edit, `x` delete (confirm). Fields: name
  (slug), lead (toggle: you / rostered agents), members (CSV of
  rostered agent ids) — the former ORG-pane forms, riding kit.Form.
- Writes go through `org.CreateTeam/UpdateTeam/DeleteTeam`
  (persist-before-visibility); team changes ping org.Subscribe, which
  rebuilds the CHANNELS rail (#<team> channels) live.

### PACKS (moved from workspace)

- Rows: installed packs from `.dhi/marketplace.json` provenance:
  `name · version · N agents · source`.
- `i` install (source form: path or git URL, `#sub/path` scoping),
  `x`/`d` uninstall (confirm, exactly the recorded agents). The
  install flow is the F-019 path: pack.toml → pack install, else
  bare-manifest import; validate-all-before-write; named skips.

### STANDARDS (moved from workspace)

- Rows: built-ins summary, workspace layer, per-team layers, per-agent
  layers with extend/replace mode.
- `w` workspace layer, `t` team layer, `g` agent layer (CSV rules;
  ←/→ toggles extend/replace), `v` effective-block preview resolving
  builtins→layers for any agent id — same semantics, same store
  writes (`internal/agentkit/standards` refuses malformed docs, which
  block turns by name — unchanged).

### AUTOPILOTS (moved from workspace)

- Rows: `name · agent · schedule · next-due · last-result` (F-015
  columns).
- `n` new (schema-validated form), `e` arm/pause, `r` run now, `x`
  delete (confirm), `o` last transcript. Runs post to `dm:<agent>`
  and dispatch through the runtime seam exactly as today; dangling
  agent → named refusal; success-only MarkRan.

### CONFIG · AGENTS (unchanged)

- CONFIG as F-006 skeleton stands; AGENTS keeps full CRUD, the `g`
  source form, archive/restore/delete, and the reload seam. AGENTS
  gains `enter`/`v` → the agent profile rendered inline (the INSPECT
  profile re-hosted here too, one renderer with F-022).

## Wiring

- `settingsview.Deps` grows: `Autopilots *autopilot.Store`,
  `Bus *bus.Bus`, `Runtime turnHandler` (run-now dispatch),
  `Tasks *tasks.Store` (profile current-work), `StdRoot string`
  (standards layer paths). All injected from cmd/dhi; no new service
  imports.
- Autopilot **execution** (launch catch-up + tick chain) stays on the
  Workspace surface (ADR-0014 §5) so schedules fire regardless of
  where the UI lives; Settings only edits cards and triggers
  explicit run-nows.
- Every crew op still drives the one reload seam; autopilot
  writes ping `autopilot.Subscribe`; the workspace stays subscribed
  for rail/notifications.

## Acceptance criteria

- Settings `[`/`]` cycles six sections; each section's CRUD hits the
  same store APIs the workspace panes used (org crew/team ops, pack
  installer, standards writes, autopilot store) — no logic forks.
- Workspace no longer renders ORG/PACKS/STANDARDS/AUTOPILOTS/INSPECT
  content or handles their keys; the section enum is the F-021 four.
- Team create/edit/delete reflects in CHANNELS (#<team> channels)
  without restart; pack install/uninstall reflects in AGENTS +
  provenance; standards preview matches the workspace preview byte-
  for-byte for the same agent.
- Run-now from Settings refuses a dangling agent by name (ADR-0011).
- Goldens: each section (teams, packs, standards, autopilots, agents
  profile).
- `make verify` green.
