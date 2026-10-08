# ADR-0026: Setup is a boot gate; launch-time changes relaunch, never hot-swap

Date: 2026-10-08 · Status: accepted · Companion to: ADR-0004 (minimal CLI),
ADR-0011 (no silent fallbacks). Serves: F-043.

## Context

`runTUI` builds every service — workspace, bus, tasks, agent runtime,
conventions, the git runner, the ripgrep searcher, the terminal env — before
any gate runs. Anything a first-run flow writes (a new workspace, identity,
conventions) or installs (the bootstrap putting git on disk) is therefore
invisible to the services of that process. The bootstrap already had this
defect: after the first-run install DHI kept running with a nil git runner
until the user restarted it by hand.

ADR-0004 keeps the CLI to bare launch, `doctor` and `version`.

## Decision

1. **Setup is a Gate**, not an overlay and not a subcommand. It owns the
   keyboard (so its text fields need no `InputCapturer`), runs after the
   toolchain gate, and is re-runnable from the palette ("Run setup wizard").
   No `dhi setup` subcommand: ADR-0004 stands.
2. **Gates chain.** `gatechain.Chain` runs several gates behind the shell's
   single slot; a later gate starts only when the earlier one finishes.
3. **No hot-swapping of services.** When setup changed anything services
   read at launch, or the bootstrap changed which hermetic tools exist, the
   program ends and `main` starts a new `runTUI` (which re-runs `boot.Audit`).
   `runTUI` returns `relaunch bool`; a tail gate (`RelaunchGate`) compares a
   fingerprint of the hermetic tools taken at launch with the current one.
   A run that changed nothing releases the shell in place.
4. **Auto-run only for first-timers.** The wizard auto-runs when no setup has
   ever finished and the workspace is absent or un-onboarded; established
   workspaces (including a `welcome.seen`) are never interrupted.
5. **The shell normalises the space key** to `" "` before routing (Bubble Tea
   names it `"space"`, which no text input accepts). Found while exercising
   the wizard end to end: it affected every form and composer.

## Consequences

- One relaunch per first run is visible as a brief restart; in exchange every
  service sees a consistent world, with no partially-initialised states.
- Later setup steps (team, CLIs, integrations, tools) register as `Step`s and
  need no knowledge of service wiring — their writes take effect via the
  relaunch.
- Conventions edited in Settings still need a restart (the Settings flash says
  so); live reload remains out of scope.
