# ADR-0038: Self-healing means named repairs, never silent defaults

Date: 2026-10-13 · Status: accepted · Extends: ADR-0011 (no fallbacks). F-085.

## Context

The user asked for onboarding that sets itself up and repairs itself.
ADR-0011 forbids fallbacks: a broken value is refused by name, never
replaced. Both can hold only if "repair" has a precise meaning.

Real breakages seen: a `.dhi/.gitignore` missing or written by an older
DHI (new runtime state such as episodes, traces and the spend ledger then
shows up in `git status`); employees pointing at a role, persona or skill
that was deleted from the library; teams whose lead or members were
deleted. Users who finished setup before new steps existed never saw them.

## Decision

1. **A repair is a named, explained change.** Each has one sentence saying
   what is wrong and what DHI will do (`repair.Repair.What`). Nothing is
   repaired without that sentence reaching the user.
2. **Two kinds.** *Safe* repairs touch only DHI's own runtime files (today:
   adding ignore entries for DHI's runtime state). They run at launch and
   are reported on a launch card. Every other repair touches something the
   team tracks (agent manifests, the org), and waits for the user's yes on
   the same card. "Not now" leaves it; doctor and the card name it again.
3. **Repairs never invent values.** A repair removes a dangling reference
   (a missing persona, a gone team member) or hands a lead back to the
   user. It never guesses a replacement. Malformed config still blocks
   boot (ADR-0011).
4. **Existing users catch up.** Setup records skipped steps as well as
   applied ones. A finished setup offers each step added since (project,
   you) once, while it applies and was never applied or skipped.
5. **Doctor shows the same list** (`repairs/<id>`) with when DHI will act.

## Consequences

- New runtime state goes into `setup.RuntimeEntries`, and every existing
  workspace picks it up through the gitignore repair.
- A repair that git already covers (a root `.gitignore`) is not offered.
- Repairs that need judgement (choosing a replacement persona, rewriting
  a broken config) stay manual, with the fix named.
