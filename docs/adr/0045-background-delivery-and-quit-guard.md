# ADR-0045: Background messages reach every surface; quitting asks about unsaved work

Date: 2026-10-31 · Status: accepted · Amends: the `surfaces.Surface` contract (ADR-0001 shell
routing). M37, F-115.

## Context

The shell passed each non-key message to the visible surface only. But every surface keeps its
own event listener going by re-arming it from its own `Update`. So once the user left a view,
that view's chain of events ended for the rest of the session:
- Workspace stopped live-refreshing, and its autopilot schedules stopped firing.
- A Settings install stayed busy.
- The editor's terminal pump filled its channel and blocked the running program.

Separately, ctrl+c and ctrl+q quit at once, even with buffers that had unsaved edits.

## Decision

1. **The shell passes every background message (anything that isn't a key, mouse, paste or
   resize event) to every surface, and to the active gate.**
   - Message types are private to the package that defines them, so a surface ignores what it
     doesn't own.
   - A new message type shared between packages needs an owner tag before it may be passed this
     way.
2. **New optional seam, `surfaces.Unsaved`** (`Unsaved() []string`, `SaveAll() error`).
   - Every quit path goes through `App.requestQuit`.
   - If anything is unsaved, a dialog lists it: save all, quit without saving, or cancel.
   - If saving fails, the dialog shows the error and DHI stays open.

## Consequences

- Surfaces must keep their message types private to their package. A shared type would now be
  handled by every surface.
- Background work runs while a view is hidden. Surfaces must not assume they are visible when an
  event arrives.
