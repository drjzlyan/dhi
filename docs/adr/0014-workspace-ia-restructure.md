# ADR-0014: Workspace IA restructure — dashboard floor, management in Settings

Date: 2026-09-10 · Status: accepted · Companion to: ADR-0011 (no silent
fallbacks), ADR-0004 (minimal CLI). F-021 (dashboard), F-022 (Slack
channels), F-023 (settings management), F-024 (theme/dialog refresh)
are the implementation.

## Context

The Workspace surface grew into nine sections (MEMBERS · ORG · PACKS ·
STANDARDS · CHANNELS · TASKS · INSPECT · AUTOPILOTS · INBOX) stacked
behind one `[`/`]` switcher. Three problems follow:

1. **Two of those sections are app management, not workspace
   operations.** ORG (team CRUD + agent create/archive) and PACKS
   (install/uninstall) are configuration; they duplicate what Settings
   AGENTS (F-018) and the `g` source-import flow (F-019) already do —
   two forms for the same roster, the ORG one weaker (no runtime/tools
   fields).
2. **TASKS is a list, not a board.** The store has carried everything
   a kanban needs since P4 (statuses, assignee, team, thread binding,
   runs rollup, PR fields, change subscription), but the view is one
   line per card plus a detail line.
3. **CHANNELS reads as a section, not a floor.** The rail is one
   horizontal text line; threads replace the transcript instead of
   sitting beside it.

The user's product shape: Workspace should be a JIRA-like dashboard
(board + notifications + team chat + repos); agent/team management
belongs in Settings, not the workspace.

## Decision

1. **Workspace has exactly four sections** — INBOX · BOARD ·
   CHANNELS · REPOS — in that rail order. BOARD is the default
   landing section (the dashboard is the product; the boot hero and
   gate flow are unchanged).
2. **BOARD** (replaces TASKS) renders the task store as a four-column
   kanban (backlog / active / in-review / done) with a task detail
   pane. Existing task keys are preserved. A bound thread opens in
   CHANNELS (see #3) — the "see the agents working on it" jump.
3. **CHANNELS** becomes the Slack-like floor inside the workspace:
   left vertical channel rail (#general, #<team>, DMs, unread
   markers), center transcript + composer, right context pane (thread
   view side-by-side with the transcript, or an agent profile). The
   bus, unread-watermark, and inbox-jump contracts are unchanged.
4. **Management moves to Settings**, which gains sections: TEAMS
   (org team CRUD — the only team UI), PACKS (install/uninstall +
   provenance), STANDARDS (layer editors + preview), AUTOPILOTS (CRUD
   + arm/pause/run-now). ORG, PACKS, STANDARDS, AUTOPILOTS, and
   INSPECT sections are removed from the workspace; the agent profile
   view (formerly INSPECT) becomes the CHANNELS context pane's
   profile mode.
5. **Autopilot execution stays wired to the Workspace surface** (the
   boot view): launch catch-up and the tick chain keep arming there,
   independent of where the AUTOPILOTS *UI* lives, so scheduled runs
   fire while the user sits in any surface the way they do today.
6. **The editor keeps its ctrl+a crew sidebar** (approvals y/n,
   suggestion-apply) — the Slack floor and the in-editor sidebar are
   different tools; no channel re-hosting inside the editor.
7. **Theme/kit upgrades serve the layout**: the kit Panel top-edge
   corner bug is fixed; background tokens gain explicit section
   shades; dialogs unify on one overlay primitive (kit.Modal/kit.Form)
   replacing the four per-surface modal systems.

### Consequences

- The section enum shrinks 9 → 4; the `[`/`]` loops in tests and the
  inbox jump-seam indices are rewritten accordingly.
- Settings grows to six sections; its Deps gain the autopilot store,
  bus, runtime, and task-store seams (injected from cmd/dhi, no
  service imports added).
- ORG-pane agent forms are deleted — Settings AGENTS (F-018) is the
  single agent CRUD surface with the strict manifest form.
- All titled-panel goldens regenerate once (the corner fix changes
  every panel's top edge); layout goldens regenerate per phase.
- No data migration: `.dhi/org.toml`, task cards, channel JSONL,
  autopilot cards, and `unread.json` are untouched — this is a pure
  surface restructure.
