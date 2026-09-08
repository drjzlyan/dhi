# STATE — current position

Updated: 2026-09-08 (session 9: F-012 animation polish + reduced-motion — M7 CLOSED)

## Where we are

**M7 COMPLETE — every line done, `make verify` green.** The last open
item (animation polish + reduced-motion) shipped as F-012:
`reduced_motion` setting (strict/layered/live) drives a `theme.Motion`
switch; the bootstrap spinner renders a static `GlyphBusy` with the
tick clock fully off when reduced; view transitions (surface switch +
gate→shell release) fade in over 2×100 ms message-driven frames,
instant under reduced motion. Goldens unchanged — the fade is
styling-only and the harness strips ANSI. M8 scope is the next decision
(see open questions).

## Session 9 gotchas

1. lipgloss `Style().Render()` on a MULTI-LINE string re-pads every
   line to the longest line — `theme.Faint` must render line-by-line or
   it silently adds trailing spaces and breaks goldens (caught by
   shell_editor golden). Any future multi-line style helper: same rule.
2. `theme.Motion` is a plain variable (like `theme.Current`) with
   `SetMotion` / `MotionForTest` — not a function; call sites read
   `theme.Motion` bare.
3. Bootstrap clock bookkeeping: `Model.clockArmed` tracks the in-flight
   timer; `tick()` is a no-op while armed (no double timers — a stray
   re-arm would run the spinner at 2× speed), `tickMsg` clears it
   first. `ensureTick` (called from every pipeline event) re-arms only
   when the clock can have gone quiet: motion toggled off then back on
   mid-install.
4. Inserting a settings-view row shifts cursor positions:
   `TestTabWidthCyclesAndPersists`/`TestLineNumbersToggle`/
   `TestScrollbackBounds` navigate by `j` counts and had to move one
   row down. Any future row insertion: audit position-based tests.
5. `settings.fileLayer.ReducedMotion` is `*bool` (explicit false in a
   higher layer must override — same reason `LineNumbers` is `*bool`);
   plain bool would make `reduced_motion = false` indistinguishable
   from unset.
6. `theme.Faint` emits SGR 2 (`\x1b[2m`) unconditionally here — even
   with TERM unset/dumb or NO_COLOR set (attribute, not color) — so
   app tests may assert the escape directly.

## Gotchas carried (still load-bearing)

1. go-git Push needs a REGISTERED remote; fixtures use bare local origins.
2. Test fakes must fully implement seams; event pumps must NOT re-arm.
3. Read form fields BEFORE closeForm(); waitReply before provider.Calls().
4. bus.History(ch,0) excludes threaded rows.
5. requestTurn must call crew.Handle SYNCHRONOUSLY.
6. Policy rules are ROOT-RELATIVE (ADR-0010); glamor renders H2 `## `.
7. macOS /var→/private/var EvalSymlinks.
8. g-chords are editor-owned (textbuf drops unknown keys); gopls hover
   fences content-kept; WorkspaceEdit bottom-up; workspace/applyEdit
   auto-answered in reader, routed async; LSP flows guard on client.
9. Seatbelt deny-default profiles need the system allows (/System,
   /usr/lib, dyld caches, mach-lookup) or wrapped processes die
   cryptically; network stays policy-engine territory.
10. sandbox.go's Sandbox interface (Name/Wrap) is load-bearing — never
    redesign it casually; adapters implement it as-is.
11. runtime guards deny-all by policy default: Guard.Exec tests need
    an explicit exec allow in policy_json.
12. fuzzy.Match and Index.Rank share matchRunes so scores can't drift.
13. Gates that start work from a keypress MUST queue through TakeCmd
    (session 8: bootgate confirm → install).
14. `go run` of internal packages from /tmp fails ("use of internal
    package not allowed"); drive via a transient file INSIDE the repo,
    delete after. `go build ./cmd/dhi` drops a `dhi` binary in cwd —
    remember to delete it.
15. Settings layer semantics: zero values in a fileLayer mean UNSET
    (only non-zero/!="" overrides; bools needing explicit-false use
    *bool); strict load rejects unknown keys per-layer before merge.
16. doctor must stay runnable on broken installs: use
    settings.LoadBestEffort anywhere diagnostics read configs.
17. runtime.New REQUIRES Config.Sandbox; test harnesses inject
    sandbox.Noop{} explicitly.

## Just finished (M7 closure — F-012)

- `internal/settings`: `reduced_motion` top-level key (*bool layer,
  strict, Known()); `Apply()` flips `theme.SetMotion(!reduced_motion)`;
  round-trip + precedence + live-apply tests.
- `internal/tui/theme`: `Motion` global + `SetMotion`/`MotionForTest`;
  `Faint()` line-by-line dim (byte-identical content); `GlyphBusy "◐"`.
- `internal/tui/surfaces/bootstrap`: reduced motion ⇒ no tick clock
  (zero idle frames), static `GlyphBusy`, stray-tick no-op,
  `clockArmed`-tracked re-arm on events when motion returns mid-install.
- `internal/tui/app`: 2×100 ms fade-in on surface switch (tab/shift+
  tab/number) and on gate release (`transitionMsg` chain, `transLeft`);
  reduced motion ⇒ instant, no cmd, no dim.
- `internal/tui/surfaces/settings`: `reduced_motion` row (live toggle +
  persist); position-based navigation tests shifted.
- Docs: F-012 spec (done), ROADMAP M7 ✅.

## Next up

1. **On you:** M8 scope decision (see open questions).
2. F-010 deferred: MCP stdio spawn wrapped through the sandbox (awaits
   first MCP consumer).
3. F-009 deferred: prepareRename validation, references/definition nav,
   auto-open-and-apply for closed files, hover markdown styling.
4. Post-M6 backlog (F-004): artifact export, diagram preview, `.dhi`-
   per-root policy scoping.
5. F-012 deferred: per-transition easings, OS-level reduce-motion
   import (blocked by ADR-0005 hermeticity).

## Open questions for user

- **M8 scope:** what milestone comes after M7? Candidates on the books:
  the deferred LSP/MCP/ideator items above, or a release-hardening
  milestone (packaging, docs, self-update).
- Archive/pick actions on task cards, ideator diagram export remain
  backlog.
