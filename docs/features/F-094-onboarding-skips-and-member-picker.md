# F-094 — Onboarding: skips that don't leak, and picking member repos

Status: done (session 58)

## Why
Skipping one setup step could quietly cost the user others. Ctrl+x in a
directory with no workspace marked project, team and integrations as
skipped, so catch-up never offered them after a workspace was created.
Progress was also shared between workspaces, because the wizard fell back
to the user state. A brief or team answered (or skipped) in workspace A
then counted as answered in workspace B. Separately, creating a workspace
added every git repo found under the directory, with no way to choose.

## Behaviour
- **Member picker.** When the workspace step finds more than one repo, it
  lists them with nothing selected. Keys: `space`/`x` toggles a repo, `a`
  selects all (or clears all when everything is selected), `j`/`k` move.
  The picker keys are shown on the status row.
  - **No choice made means the default.** Enter with nothing selected adds
    every repo found, and the picker says so ("None selected · enter adds
    all N").
  - **Later means later.** `l` (choose later) leaves the step without
    creating anything: no workspace, no `.dhi/`, no defaults. The done
    step lists it as skipped, and the user returns to it through ctrl+p →
    Run setup wizard. A workspace cannot have zero members, so "later"
    cannot create an empty one.
  - The selection survives going back and forth. The list shows 10 rows at
    a time, with "↑/↓ n more" lines, and the window follows the cursor.
    With a single candidate (the directory is a repo, it holds one repo, or
    it holds none) the plain confirmation view is shown.
- **Skip setup only settles steps that could run.** Ctrl+x marks as
  skipped only the remaining steps whose `Applies()` is true. Steps that
  were waiting on a workspace stay owed.
- **Progress is scoped** (`setup.Scoped`). The workspace steps (`workspace`,
  `project`, `team`, `integrations`) come only from that workspace's
  `.dhi/setup.json`. Every other step comes from the user state.
  `Finished` is true when either state is finished.
- **Catch-up offers the team.** The team step joins the catch-up steps.
  It still applies only to a workspace with an empty roster.
- **The done step lists what was skipped.** It names each skipped step and
  says to re-run setup to pick them up. With no workspace, it says the
  brief, team and integrations are waiting for one.
- **Esc works while the identity check runs.** A slow git no longer blocks
  the wizard, and a late answer is ignored.

## Acceptance
- [x] Picker: nothing preselected; only the chosen repos are written to
      `workspace.toml`; enter with none selected adds all; `l` creates
      nothing; picks kept across back; `a` toggles all
      (`TestWorkspacePicker*`, golden `wizard_workspace_picker`).
- [x] Ctrl+x in a bare directory leaves project and team for catch-up
      (`TestSkipSetupWithoutWorkspaceLeavesWorkspaceStepsOwed`).
- [x] Another workspace's answers do not settle this workspace's steps
      (`TestScopedKeepsWorkspaceStepsToTheirWorkspace`).
- [x] Done step names the skipped steps; applying a step after going back
      removes it from that list (`TestDoneStepNamesSkippedSteps`).
- [x] Esc while checking identity skips the step
      (`TestIdentityCheckCanBeSkipped`).
- [x] Live (tmux, sandboxed HOME): with 30 repos, picking the last two
      wrote exactly those two; a second workspace under the same HOME was
      offered project and team even though the user file had them skipped.
