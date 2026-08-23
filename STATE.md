# STATE — current position

Updated: 2026-08-23 (session: five-view IA alignment)

## Where we are

M0 complete and committed (`b9c56fd`). **Five-view IA alignment implemented
and verified** (uncommitted — commit next): Workspace is the boot view with
the centered DHI brand hero; Home removed; registry = Workspace · Editor ·
Ideator · Reviewer · Settings; keys `1-5`.

## Just finished

- `internal/tui/branding`: shared gradient logo + `HeroBlock`/`Hero` (M1
  bootstrap will reuse).
- `kit.Center` exported; shell help overlay + workspace view use it.
- `surfaces/workspace`: boot surface, hero centered on its own axis with a
  column-aligned capability list beneath (all M4-tagged).
- Help overlay text now "1-5 jump between views"; statusline hints updated.
- Docs: F-002..F-006 specs complete; ROADMAP rewritten around views
  (M1 foundation → M2 Editor core → M3 runtime+chat → M4 Workspace →
  M5 Reviewer → M6 Ideator → M7 hardening); product.md IA section added.

## Gotchas learned (do not re-learn these)

1. Center styled text by **visible width** (`ansi.Strip` first) — raw rune
   counts include escape bytes and broke two tests before we caught it.
2. Center once at composition time, not per nested block (double-centering
   misaligns groups); center *groups* on a shared axis instead.
3. Bubble Tea v2 reminders still apply (see ADR-0001 notes): `tea.View`
   struct, `.AltScreen = true`, `KeyPressMsg{Text:,Code:}` for runes.

## Next up (M1 start)

1. Commit pending IA alignment (user approves diff first).
2. `internal/toolchain`: Manager skeleton + registry manifest schema +
   sha256 verifier against httptest fixtures.
3. `internal/sandbox`: path-jail policy types.
4. `internal/workspace`: multi-repo domain + `.dhi/workspace.toml` + VPath resolver.
5. Doctor check suite + `dhi doctor --json`; animated Bootstrap surface last.

## Open questions for user

- None blocking.
