# F-079 — Team by role count

Status: done (M31 P1)

## Problem
Onboarding offered three fixed teams (solo/squad/studio) built from
planner/fixer/reviewer/scout/scribe. A user could not say "one lead, three
full-stack engineers, a tester and a product manager", and there were no
lead, full-stack, frontend, backend, tester or product-manager roles.

## Behaviour
- **Roles.** The built-in library gains `lead`, `fullstack`, `frontend`,
  `backend`, `tester`, `product-manager`, `designer` and `devops`. Each has
  its own tools and sandbox preset, like the existing roles.
- **Compose.** `starter.Compose(counts, taken)` turns role counts into one
  template per lead:
  - with N leads, the other employees are dealt round-robin across N teams;
  - with no lead, there is one team led by you (`org.Human`);
  - ids and names come from a fixed name pool, skipping any id that is
    already used or archived, so a second run never collides;
  - personas rotate per role (`starter.PersonaFor`).
- **ApplyAll.** It applies every composed team through `starter.Apply`. If
  a later team fails, the teams and agents written before it are removed:
  all or nothing.
- **Wizard.** The team step asks "preset or custom". Custom shows one count
  per role (left/right changes it, 0–9) and a live summary ("2 teams · 7
  employees"). The default is 1 lead, 2 full-stack, 1 tester. Enter accepts.
- **Settings → TEAMS.** `a` adds employees by role with the same count form,
  so the crew can grow after onboarding.

## Acceptance
- [x] New role cards parse and resolve against the built-in library
- [x] Compose: split across leads, no-lead team, id collisions, archived ids
- [x] ApplyAll rolls back earlier teams when a later one fails
- [x] Wizard custom counts create the teams; goldens
- [x] Settings TEAMS `+` adds employees by role; goldens

## Found on the way
Settings form dialogs (new/edit team, templates, library, persona) drew an
empty box: the modal never received the form's rows. `View` now renders the
open form as the dialog body; `settings_teams_compose` is the first golden
of a form dialog.
