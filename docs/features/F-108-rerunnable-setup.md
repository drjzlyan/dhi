# F-108 — Re-runnable onboarding

Status: planned (session 61, M35 P8) · amends F-098

## Why
Finished setup steps could not be reopened except through a hidden palette entry, re-runs did not
show the current values, and single-step runs had no back.

## Behaviour
- The start-page checklist lists every step (✓ done, – skipped, ○ pending); `j/k`, enter or a
  click opens any of them.
- Settings → SETUP lists the same steps with Re-run, and "Reset and run the wizard again" (clears
  step records only, never config).
- Re-runs show the current values; saving updates in place; esc leaves the step and its record as
  they were.
- The wizard's stepper is clickable; `ctrl+b`, `alt+left` and `‹ Back` go back.
- A new optional Skills step recommends official plugins and skills for the team's roles.

## Acceptance
- [ ] Re-run of a done step is prefilled and keeps ✓ on esc.
- [ ] Reset keeps config files byte-identical.
- [ ] Stepper click jumps to an earlier step.
