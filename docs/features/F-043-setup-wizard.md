# F-043: Setup wizard + animation kit

Status: complete (M25 P2) · ADR-0026 · Depends on: F-042 (conventions), F-012
(motion), F-011/ADR-0011 (no silent fallbacks) · Later phases register
steps (team, CLIs, integrations, tools, tutorials) without touching the core.

## Problem

First run ends at a static key-hint card. A user in a plain directory sees
"no workspace loaded"; nothing creates a workspace, collects the git
identity commits require, or records the team's conventions.

## Design

### Animation primitives (`internal/tui/kit/anim.go`) — pure
No timers inside kit; the caller owns the clock and passes a frame index.
- `SpinnerGlyph(frame)` — braille frames; static `◐` under reduced motion.
  The bootstrap surface now uses it (its goldens must not change).
- `ProgressBar{Width, Fraction}` — theme-`Blend` gradient fill; static glyphs.
- `Typewriter(text, n)` — first n runes; **reduced motion returns the full text**.
- `Stepper` — `✓ welcome › ◐ identity › ○ team`; collapses to `2/5 identity` when narrow.

### Wizard (`internal/tui/surfaces/wizard`)
A **Gate** (same contract as bootgate): it owns the keyboard, so text fields
need no `InputCapturer`. It is a *gate*, not an overlay, for both auto-run and
palette re-run (`App.StartGate`).

```go
type Step interface {
    ID() string
    Title() string
    Applies(*Env) bool                 // shown in this run?
    Enter(*Env) tea.Cmd                // became active
    HandleKey(key string) Action       // Stay | Next | Back | Skip
    Update(tea.Msg) tea.Cmd            // async results
    View(w int, frame int) []string
    Apply(*Env) error                  // persist on Next; failure keeps the step
}
```
`Env` injects services as funcs (identity get/set, settings paths, workspace
root) — the package imports no concrete service state. Later phases append
steps via `setup.New(..., extraSteps...)`.

Steps in P2: **welcome** (typewriter intro) → **workspace** (only without
one: discovered members, create) → **identity** (shows resolved git identity or
collects name/email; shows the exact `git config --global …` commands and runs
them through the hermetic git only after the user confirms) → **conventions**
(preset: commit format, branch pattern, co-author, copyright holder; writes
`.dhi/conventions.toml`, the tracked team layer) → **done**.

### Persistence
`internal/setup`: `State` JSON at `<config dir>/dhi/setup.json` (user scope:
finished, per-step completion) and `.dhi/setup.json` (workspace scope,
gitignored). `ShouldAutoRun`: auto-run only when there is **no workspace and
setup never finished**, or a workspace with **no setup state and no
`welcome.seen`** — existing workspaces (including this repo) are never pushed
through it. Otherwise it is available from the palette: **Run setup wizard**.
There is deliberately **no `dhi setup` subcommand** (ADR-0004 stays: bare
launch, `doctor`, `version`).

### Workspace init
`workspace.CreateWith(root, members map[name]path)` (the existing `Create`
writes `path = name`; init-in-place needs `path = "."`). `setup.DiscoverMembers`:
child dirs holding `.git`; if root is itself a repo (or none are found) a single
member named for the sanitised basename at `.`. `setup.InitWorkspace` also writes
`.dhi/.gitignore` (channels, memory, tasks, sessions, reviews, knowledge, …,
`agents/*/runs/`) so new users do not commit their chat logs.

### Relaunch loop
`runTUI` builds every service before any gate runs, so nothing the wizard
writes is visible in that process. We do **not** hot-swap services (ADR-0026):
when the wizard changed anything it ends the program and `main` re-runs
`boot.Audit` and `runTUI` (`runTUI` returns `relaunch bool`). A palette re-run
that changed nothing just returns to the shell.

The same loop fixes a latent bug: after the first-run bootstrap installed git
and ripgrep, the running services (captured at launch) never saw them. A tail
`gatechain.RelaunchGate` compares a fingerprint of `bin/{git,rg,go,gopls}` at
launch with the current one and restarts when it changed.

### Gate chain
`App` has one gate slot. `gatechain.Chain` implements `Gate` over a list
(toolchain gate, then setup), forwarding `TakeCmd`, and `Finished` is true
only after the last gate. The welcome card is skipped when the wizard runs.

## Bug found while testing end to end
Bubble Tea names the space bar `"space"`; every text input accepts only single
printable runes, so **no form, composer or insert mode could receive a space**
(the identity came out `AdaLovelace`). The shell now normalises a bare space to
`" "` (`keyString`); the two places that matched `"space"` accept both. Tested
through `App.Update`, forms and textbuf.
Also: the shell never checked `Gate.Finished()` after a *key press*, so a gate
finished by a key only released on the next message — invisible while a spinner
ticked, a hang under reduced motion. `releaseGate` now runs after keys too.

## Acceptance criteria
- [x] kit primitives are pure; reduced motion: no clock, typewriter complete, static progress
- [x] bootstrap goldens unchanged after lifting the spinner
- [x] Goldens for stepper/progress/typewriter in both motion modes
- [x] Gate chain: second gate starts only after the first finishes; verified through `App.Update`
- [x] Wizard: step order, back/skip, Apply failure keeps the step, async result handling
- [x] Workspace init writes `path = "."` members, a valid `workspace.Load`, and `.dhi/.gitignore`
- [x] Identity writes only after confirmation, via the hermetic git runner; failure is shown
- [x] Conventions step writes `.dhi/conventions.toml` that `settings.Load` accepts
- [x] Auto-run rules (table-tested); existing workspaces are not interrupted
- [x] Palette "Run setup wizard"; relaunch only when something changed
- [x] Walked end to end in tmux (fresh HOME, empty dir): wizard → workspace → identity → conventions → relaunch into the new workspace; palette re-run
- [x] `make verify` green

## Deferred
Team, CLI, integrations, tools and tutorial steps (F-044+); live service reload.
