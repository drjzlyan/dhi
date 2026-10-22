# F-108 — Re-runnable onboarding

Status: done (session 61, M35 P8) · amends F-098

## Why
Finished setup steps could not be reopened except through a hidden palette entry, re-runs did not
show the current values, and single-step runs had no back.

## Behaviour
- The start-page checklist lists every step (✓ done, – skipped, ○ pending); a click opens any of
  them and `c` runs the next pending one. Once all are settled it keeps one line, "SETUP ✓ … · c
  change a step", which opens Settings → SETUP. The palette's "Set up: <step>" still lists every step.
- Settings → SETUP lists the same steps with Re-run, and "Reset and run the wizard again" (clears
  step records only, never config).
- Re-runs show the current values (the project step loads the saved brief; you, instructions and
  conventions read your config); saving updates in place; esc leaves the step and its record as
  they were ("esc leave as is"). Steps whose first-run condition no longer holds (a brief, a profile,
  instructions, a team exist) still re-run (`CanRerun`); creating a workspace cannot re-run inside one.
- The wizard goes back with `ctrl+b`, or Option+← (alt+left) on steps that are not taking text.
  A clickable stepper was left out: the card's geometry is not recorded for clicks.
- The integrations step offers the vendors' official plugins (F-105); skills install from SKILLS.

## Acceptance
- [x] Re-run of a done step is prefilled and keeps ✓ on esc.
- [x] Reset clears step records only (Settings test; config is not touched by the reset path).
- [ ] Stepper click jumps to an earlier step (not done; ctrl+b / alt+← go back).
