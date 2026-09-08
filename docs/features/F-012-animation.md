# F-012: Animation polish + reduced-motion

Status: done (M7, 2026-09-08) · Milestone: M7 · Closes: last open M7 line

## Summary

The only animations DHI ships are the bootstrap spinner (120 ms braille
frames) and zero view transitions (surfaces swap instantly). This feature
polishes both and makes every animation honor a **reduced-motion**
preference so DHI is comfortable for users who are sensitive to motion
(vestibular disorders, photosensitivity, battery, low-bandwidth terminals).

Design constraints (unchanged):

- **Deterministic rendering under test.** All animation stays
  message-driven (explicit `tea.Tick` re-arms); no goroutine clocks, no
  frame counters advanced from wall time.
- **No new system dependencies (ADR-0005).** Reduced motion is NOT read
  from the OS accessibility setting (that would shell out to
  `defaults`/`gsettings`). It is a DHI setting, decided by the user in
  DHI's config file.
- **No silent fallbacks (ADR-0011).** The setting is strict like every
  other key; unknown values refuse boot.

## Part A — the `reduced_motion` setting

- **Schema:** new top-level key `reduced_motion` (bool, default `false`),
  alongside `theme` (it is a UI preference, not an editor/terminal
  one). Zero value in a layer means UNSET (false == default, so a plain
  bool is sufficient; no pointer needed). `Known()` updated, strict
  load/validate unchanged in shape.
- **Apply:** `Config.Apply()` (the live-application seam already used for
  theme swap) also flips a new `theme.Motion` global, so every surface
  reads one source of truth — same pattern as `theme.Current`.
  Toggling from the Settings view takes effect immediately, no
  restart.
- **Settings view:** new row `ui.reduced_motion` between theme and
  editor rows; ←/→ flips it live + persists like the other rows.

### Acceptance criteria (Part A)

- `reduced_motion = true` parses, layers (user > workspace), saves
  round-trip, and is not flagged by `UnknownKeys`/doctor.
- `Apply()` sets `theme.Motion` to `!reduced_motion`; default build has
  motion ON.
- Settings view row flips the value live and persists.
- Unit tests cover precedence + round-trip + live apply.

## Part B — theme motion seam (`internal/tui/theme`)

- **`var Motion = true`** — global switch for animated effects;
  `SetMotion(bool)` for production (settings.Apply) and
  `MotionForTest(t, bool)` with t.Cleanup restore for tests (same shape
  as `SwapForTest`).
- **`GlyphBusy = "◐"`** — the static activity indicator shown in place
  of an animated spinner when motion is off (basic geometric block,
  font-safe, theme-owned per the branding rule).
- **`Faint(s string) string`** — renders a string dimmed (lipgloss
  `Faint`), the transition dim used by the shell. Kept in theme so
  surfaces/app never construct raw styles or colors (lint rule).

## Part C — bootstrap under reduced motion

- Motion ON (default): unchanged — braille spinner advances per tick.
- Motion OFF:
  - `Init()` does **not** arm the tick clock at all (no idle re-arms,
    no wasted frames).
  - Active rows render the static `theme.GlyphBusy` glyph.
  - `tickMsg` handling is a no-op when motion is off (defensive; the
    clock is not running).
  - If the user re-enables motion mid-install, the next pipeline event
    re-arms the clock (an event handler returns the tick cmd when the
    install is running and motion is on), so the spinner never sits
    frozen while work continues.
- Completion/failure transitions (active → ✓/✗) are unchanged — they
  are state changes, not animation.

### Acceptance criteria (Part C)

- Motion ON: existing spinner tests pass unchanged (frame advance,
  clock re-arm, clock stops on done/fail).
- Motion OFF: `tick()` returns nil; a running row shows `GlyphBusy`
  and never a braille frame; `tickMsg` does not advance state.
- Motion OFF → ON mid-install: an event re-arms the clock.

## Part D — surface transitions (app shell)

- **Switch transition:** when the active surface changes (tab,
  shift+tab, number key) with motion ON, the new body renders dimmed
  for two 100 ms frames (~200 ms total) before settling full-strength —
  a fade-in that reads as "this view just arrived". Implemented as a
  `transitionMsg` tick chain (message-driven, testable); the key
  handler returns the first tick cmd through the existing
  `handleGlobal → tea.Cmd` return path. Rapid re-switches restart the
  fade (frame counter reset).
- **Gate release:** when a gate finishes (bootstrap done/failed → shell
  resumes), the same fade-in runs on the first shell frame, so the
  bootstrap→shell handoff is a transition, not a cut.
- **Motion OFF:** both are instant — no cmd returned, no dim, zero
  frames. There is nothing to reduce; the state is exactly today's
  behavior.
- Dimming is whole-body via `theme.Faint` (SGR dim, not a color — the
  raw-color lint rule is unaffected). Content is byte-identical during
  a fade, so **goldens are unaffected** (the harness strips ANSI).

### Acceptance criteria (Part D)

- Motion ON: a surface switch returns a tick cmd; the body is dimmed
  for exactly two transition frames; after the last frame no cmd is
  returned and the body is undimmed. Re-switching mid-fade restarts it.
- Motion OFF: a surface switch returns nil cmd; the body is never
  dimmed.
- Gate release starts the fade (motion ON) / is instant (motion OFF).
- Existing shell goldens unchanged.

## Part E — what is deliberately NOT animated

- Statusline, tabs, panels, lists: stateful, not temporal — no
  animation.
- Modals/popups (help, approvals, hover, completion): appear on demand
  while the user is looking; animating them adds latency to input
  feedback. Deferred until a real consumer complains.
- Streaming chat/PTY output: already paced by data arrival.

## Deferred

- Per-transition custom easings (one fade shape is enough for v1).
- Terminal-capability detection (truecolor vs dim fallback) — lipgloss
  already degrades `Faint` gracefully per terminal env.
- OS-level reduce-motion import (blocked by ADR-0005 hermeticity;
  revisit only if a seam appears that reads env vars at boot).
