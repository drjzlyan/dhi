# F-085 — Self-healing onboarding

Status: done (M31 P7) · ADR-0038

## Behaviour
- **Repairs** (`internal/repair`): `Find` lists what DHI can fix, `Run`
  applies them (all, or only the safe ones).
  - *gitignore* (safe): writes `.dhi/.gitignore` when missing, or appends
    the runtime entries an older DHI did not know (episodes, traces,
    proposals, spend ledger, grants, evals, tool usage). Entries git already
    ignores (checked with `git check-ignore` through hermetic git) are
    skipped. `setup.RuntimeEntries` is the one list, shared with new
    workspaces.
  - *library refs* (ask): an employee whose role, persona or skill no longer
    exists has that reference dropped; its own instructions and tools stay.
  - *dangling team* (ask): a team whose lead was deleted is led by you;
    deleted members are dropped. Archived employees are not "gone".
- **Launch card** (`repairgate`): runs safe repairs, then shows "Fixed on
  its own" and "Can fix, with your OK". Enter fixes the rest and shows the
  outcome; esc leaves them. Shown only when there is something to say.
- **Catch-up setup:** `setup.State.Skipped` records skipped steps;
  `wizard.CatchUpSteps` offers each step added since a finished setup
  (project, you) once, followed by the closing step.
- **Doctor:** `repairs/<id>` rows say what will happen and when.
- The DHI repo itself: `.dhi/setup.json` (per-user runtime state) was
  tracked; it is now ignored and untracked.

## Acceptance
- [x] gitignore: written, then complete; older file topped up, user lines kept; skipped when git ignores
- [x] library refs: asks, drops only what is missing; safe run leaves agents alone
- [x] dangling team: lead → you, missing members dropped
- [x] launch card: ask → fix with failures named → continue; not now runs nothing; golden
- [x] catch-up: steps settled by apply or skip are never offered again
