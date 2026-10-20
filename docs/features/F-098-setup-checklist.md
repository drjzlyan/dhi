# F-098 — Setup as a guided checklist, with interactive steps

Status: planned (M34 P4–P5) · ADR-0041

## Why
First run was an eleven-step wizard gate in front of the app. Several steps were text walls or raw
forms: rules separated by `;`, a literal `\n` for new lines, and field explanations shown only as
dim lines at the bottom.

## Behaviour
- Boot, bootstrap and repair gates still block. The setup wizard no longer auto-runs, and catch-up
  is no longer a gate.
- `setup.Checklist` lists the steps that apply, each with a title, a why, and done, skipped or
  pending. It is built from per-step records, never from `Finished`.
  - Migration: an existing user with `Finished` set and no step records has every step settled,
    except the catch-up steps (project, you, instructions, team) that still apply.
- The checklist appears on the start page, as a `setup N/M` status pill (clickable), and in the
  palette as "Continue setup". A toast appears when everything is done.
- `wizard.Single(step)` runs one step as a focused dialog. It marks or skips that step only and
  never marks setup finished. A step that needs a restart offers the existing relaunch. The linear
  wizard stays available as "Run setup wizard".
- Each step has a one-line why and a time estimate. Keys appear only on the status row. Per step:
  - welcome: three cards
  - identity: a card with Use/Change, and Input with suggestions
  - project: help for the focused field, and multi-line Input
  - instructions: a list editor with suggested rules
  - conventions: a live preview of the branch, commit and trailer
  - cli and integrations: cards with badges and buttons
  - team: roster cards
  - result: "what to try next" cards

## Acceptance
- [ ] Table test for `setup.Checklist`, covering the migration rule.
- [ ] Running a single step leaves the other items pending; `Finished` is never set.
- [ ] First launch with no setup lands in the Editor, with no wizard gate.
- [ ] F-094's skip, back and esc semantics are kept (existing tests pass).
- [ ] Goldens for each upgraded step.
